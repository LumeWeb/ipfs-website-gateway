package event

import (
	"time"

	ipfs "go.lumeweb.com/ipfs-sdk"
	"go.uber.org/zap"
)

// ReconnectConfig holds reconnection parameters for the SSE client.
type ReconnectConfig struct {
	Reconnect  bool
	Backoff    time.Duration
	MaxBackoff time.Duration
	MaxRetries int
}

// PortalEventClient connects to the portal SSE website event stream. It wraps
// the ipfs-sdk WebsiteEventsClient, which owns the connection, reconnection and
// durable Last-Event-ID replay, and wires the SDK's observability callbacks to
// the gateway's Prometheus metrics.
type PortalEventClient struct {
	client *ipfs.WebsiteEventsClient
}

// NewPortalEventClient creates an SSE client for portal website lifecycle
// events backed by ipfs.NewWebsiteEventsClient. Pass the portal base URL; the
// client appends /internal/websites/events and authenticates with the gateway
// secret. onConnected, if non-nil, is invoked on every connection-state
// transition (disconnected/reconnected) so callers can trigger reconciliation.
// The returned client is not running; call Start to connect.
func NewPortalEventClient(baseURL, secret string, cfg ReconnectConfig, handler ipfs.WebsiteEventHandler, onConnected func(connected bool), logger *zap.Logger) (*PortalEventClient, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	logger = logger.Named("sse")

	client, err := ipfs.NewWebsiteEventsClient(baseURL, secret,
		ipfs.WithWebsiteEventsReconnect(cfg.Reconnect),
		ipfs.WithWebsiteEventsBackoff(cfg.Backoff, cfg.MaxBackoff),
		ipfs.WithWebsiteEventsMaxRetries(cfg.MaxRetries),
		ipfs.WithWebsiteEventsLogger(logger),
		ipfs.WithWebsiteEventsStats(wireSSEStats(logger, onConnected)),
		// Keep the sse_connected gauge fresh during backoff and while streaming.
		ipfs.WithWebsiteEventsStatsPollInterval(5*time.Second),
	)
	if err != nil {
		return nil, err
	}

	client.OnEvent(handler)
	client.OnError(func(err error) {
		logger.Error("SSE client gave up reconnecting", zap.Error(err))
		sseErrorsTotal.Inc()
	})

	return &PortalEventClient{client: client}, nil
}

// Start connects to the SSE endpoint in a background goroutine. Must be called
// only once; subsequent calls are no-ops.
func (c *PortalEventClient) Start() {
	c.client.Start()
}

// Stop disconnects from the SSE endpoint and waits for in-flight handlers to
// drain.
func (c *PortalEventClient) Stop() {
	c.client.Stop()
	sseConnected.Set(0)
}

// IsConnected returns true if the SSE client is currently connected.
func (c *PortalEventClient) IsConnected() bool {
	return c.client.IsConnected()
}

// LastEventID returns the most recently received durable event ID.
func (c *PortalEventClient) LastEventID() string {
	return c.client.LastEventID()
}

// wireSSEStats maps the SDK's SSE observability callbacks to the gateway's
// Prometheus metrics and forwards connection-state transitions to onConnected.
func wireSSEStats(logger *zap.Logger, onConnected func(connected bool)) ipfs.WebsiteEventsStats {
	return ipfs.WebsiteEventsStats{
		EventReceived: func() {
			sseEventsReceivedTotal.Inc()
		},
		ParseError: func() {
			sseParseErrorsTotal.Inc()
		},
		ConnectionError: func(err error) {
			logger.Error("SSE client error", zap.Error(err))
			sseErrorsTotal.Inc()
		},
		Connected: func(connected bool) {
			if connected {
				sseConnected.Set(1)
			} else {
				sseConnected.Set(0)
			}
			if onConnected != nil {
				onConnected(connected)
			}
		},
	}
}
