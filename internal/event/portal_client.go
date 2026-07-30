package event

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/avast/retry-go/v5"
	sseServer "github.com/apt304/sse-go/server"
	"go.uber.org/zap"
)

// WebsitePublishedEvent is the payload for site_published events.
type WebsitePublishedEvent struct {
	Domain      string `json:"domain"`
	CID         string `json:"cid"`
	PublishedAt string `json:"published_at"`
}

// WebsiteRemovedEvent is the payload for site_removed events.
type WebsiteRemovedEvent struct {
	Domain    string `json:"domain"`
	RemovedAt string `json:"removed_at"`
}

// EventType is the type of website lifecycle event from the portal.
type EventType string

const (
	EventTypePublished EventType = "site_published"
	EventTypeRemoved    EventType = "site_removed"
)

// SSEEvent wraps a website event with a type field for client-side consumption.
// This matches the portal's event.SSEEvent struct.
type SSEEvent struct {
	Type EventType     `json:"type"`
	Data json.RawMessage `json:"data"`
}

// WebsiteEvent is the parsed event ready for handler dispatch.
type WebsiteEvent struct {
	Type      EventType
	Published *WebsitePublishedEvent
	Removed   *WebsiteRemovedEvent
}

// PortalEventHandler processes a parsed website event.
type PortalEventHandler func(event WebsiteEvent)

// ReconnectConfig holds reconnection parameters for the SSE client.
type ReconnectConfig struct {
	Reconnect  bool
	Backoff    time.Duration
	MaxBackoff time.Duration
	MaxRetries int
}

// PortalEventClient connects to the portal SSE endpoint and routes events.
// It wraps the vendored sse-go Client (client.go) with header injection
// and website-specific event parsing.
type PortalEventClient struct {
	client    *Client
	handler   PortalEventHandler
	logger    *zap.Logger
	cfg       ReconnectConfig
	startOnce sync.Once
	wg        sync.WaitGroup
}

// NewPortalEventClient creates a new SSE client for portal website events.
// The client is not started; call Start() to connect.
func NewPortalEventClient(url, secret string, cfg ReconnectConfig, handler PortalEventHandler, logger *zap.Logger) *PortalEventClient {
	if logger == nil {
		logger = zap.NewNop()
	}
	log := logger.Named("sse")

	options := Options{
		Reconnect:  cfg.Reconnect,
		Backoff:    cfg.Backoff,
		MaxBackoff: cfg.MaxBackoff,
		MaxRetries: cfg.MaxRetries,
	}

	client := NewClient(url, []string{"gateway"}, options)
	client.SetHeader("X-Gateway-Secret", secret)

	portalClient := &PortalEventClient{
		client:  client,
		handler: handler,
		logger:  log,
		cfg:     cfg,
	}

	// Route site_published and site_removed events through the handler
	routeEvent := func(ev sseServer.Event, log *zap.Logger) {
		sseEventsReceivedTotal.Inc()

		parsed, err := parseWebsiteEvent(ev)
		if err != nil {
			log.Error("failed to parse SSE event",
				zap.String("event_type", ev.Type),
				zap.Error(err))
			sseParseErrorsTotal.Inc()
			return
		}

		if portalClient.handler != nil {
			portalClient.handler(parsed)
		}
	}

	client.OnEvent(string(EventTypePublished), func(ev sseServer.Event) {
		routeEvent(ev, log)
	})
	client.OnEvent(string(EventTypeRemoved), func(ev sseServer.Event) {
		routeEvent(ev, log)
	})
	client.OnEvent("error", func(ev sseServer.Event) {
		log.Error("SSE parse/error event from connection",
			zap.String("data", string(ev.Data)))
		sseParseErrorsTotal.Inc()
	})
	client.OnError(func(err error) {
		log.Error("SSE client error", zap.Error(err))
		sseErrorsTotal.Inc()
	})

	return portalClient
}

// Start connects to the SSE endpoint in a goroutine.
// Must be called only once; subsequent calls are no-ops.
func (c *PortalEventClient) Start() {
	c.startOnce.Do(func() {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()

			// Start the metric poller before the connect retry loop.
			// This avoids the race where Disconnect() closes done
			// before the poller's wg.Add runs.
			c.wg.Add(1)
			go func() {
				defer c.wg.Done()
				ticker := time.NewTicker(5 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-c.client.done:
						return
					case <-ticker.C:
						if c.client.IsConnected() {
							sseConnected.Set(1)
						} else {
							sseConnected.Set(0)
						}
					}
				}
			}()

			// Retry initial connection with bounded, jittered backoff.
			// Uses avast/retry-go for consistency with the prewarmer.
			// MaxRetries=0 means unlimited (retry-go default).
			r := retry.New(
				retry.Attempts(uint(c.cfg.MaxRetries)),
				retry.Delay(c.cfg.Backoff),
				retry.MaxDelay(c.cfg.MaxBackoff),
				retry.DelayType(retry.CombineDelay(retry.BackOffDelay, retry.RandomDelay)),
				retry.LastErrorOnly(true),
				retry.OnRetry(func(n uint, err error) {
					c.logger.Error("SSE connect failed, retrying",
						zap.Error(err),
						zap.Uint("attempt", n+1))
				}),
			)
			err := r.Do(func() error {
				if err := c.client.Connect(); err != nil {
					if err == ErrClientClosed {
						return retry.Unrecoverable(err)
					}
					sseErrorsTotal.Inc()
					return err
				}
				return nil
			})
			if err != nil && err != ErrClientClosed {
				c.logger.Error("SSE initial connect exhausted retries", zap.Error(err))
			}
		}()
	})
}

// Stop disconnects from the SSE endpoint and waits for all handler
// goroutines to drain. This ensures no in-flight event handler can
// race with downstream shutdown (e.g. prewarmer.Stop()).
func (c *PortalEventClient) Stop() {
	c.client.Disconnect()
	c.client.Wait() // wait for handleEvents goroutine
	c.wg.Wait()     // wait for Start goroutine (initial connect retry loop)
	sseConnected.Set(0)
}

// IsConnected returns true if the SSE client is currently connected.
func (c *PortalEventClient) IsConnected() bool {
	return c.client.IsConnected()
}

// parseWebsiteEvent decodes the SSE event data into a WebsiteEvent.
// The portal wraps events as SSEEvent{Type, Data}, where Data is the
// JSON-encoded WebsitePublishedEvent or WebsiteRemovedEvent.
func parseWebsiteEvent(ev sseServer.Event) (WebsiteEvent, error) {
	var sseEvent SSEEvent
	if err := json.Unmarshal(ev.Data, &sseEvent); err != nil {
		return WebsiteEvent{}, fmt.Errorf("unmarshal SSE event wrapper: %w", err)
	}

	result := WebsiteEvent{Type: sseEvent.Type}

	switch sseEvent.Type {
	case EventTypePublished:
		var pub WebsitePublishedEvent
		if err := json.Unmarshal(sseEvent.Data, &pub); err != nil {
			return WebsiteEvent{}, fmt.Errorf("unmarshal published event: %w", err)
		}
		result.Published = &pub
	case EventTypeRemoved:
		var rem WebsiteRemovedEvent
		if err := json.Unmarshal(sseEvent.Data, &rem); err != nil {
			return WebsiteEvent{}, fmt.Errorf("unmarshal removed event: %w", err)
		}
		result.Removed = &rem
	default:
		// Unknown event type — not an error, just skip
	}

	return result, nil
}
