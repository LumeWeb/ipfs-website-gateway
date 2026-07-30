// Package event provides an SSE client for portal website lifecycle events.
//
// This file is adapted from github.com/apt304/sse-go/client/client.go (v0.0.3)
// with one modification: added SetHeader() support so the gateway can inject
// the X-Gateway-Secret header required by the portal's gatewayAuthMw.
// The upstream Client creates Connection internally in Connect() with no way
// to set headers before the initial HTTP request.

package event

import (
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	sse "github.com/apt304/sse-go/server"
)

// ErrClientClosed is returned by Connect() when the client has already
// been disconnected via Disconnect(). The Client is single-use.
var ErrClientClosed = fmt.Errorf("client has been closed, cannot reconnect")

// EventHandler is a function that handles SSE events
type EventHandler func(event sse.Event)

// ErrorHandler is a function that handles errors
type ErrorHandler func(err error)

// ConnectionState represents the current connection state
type ConnectionState int

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateReconnecting
)

// Options configures the SSE client
type Options struct {
	Reconnect  bool          // Enable automatic reconnection
	Backoff    time.Duration // Initial reconnection backoff
	MaxBackoff time.Duration // Maximum reconnection backoff
	MaxRetries int           // Maximum number of reconnection attempts (0 = unlimited)
}

// Client represents an SSE client with automatic reconnection.
// Adapted from github.com/apt304/sse-go/client.Client — adds header injection
// via SetHeader() so callers can set authentication headers before Connect().
type Client struct {
	mu        sync.RWMutex
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	state   ConnectionState
	stateMu sync.RWMutex

	conn   *Connection
	connMu sync.Mutex

	url     string
	topics  []string
	options Options

	// headers holds custom HTTP headers applied to each Connection.
	// This is the only addition vs upstream sse-go client.Client.
	headers map[string]string

	lastEventID    string
	lastEventIDMu  sync.RWMutex
	eventHandlers  map[string][]EventHandler
	messageHandler EventHandler
	errorHandler   ErrorHandler
}

// NewClient creates a new SSE client
func NewClient(url string, topics []string, opts ...Options) *Client {
	options := Options{
		Reconnect:  true,
		Backoff:    1 * time.Second,
		MaxBackoff: 30 * time.Second,
		MaxRetries: 10, // Default to 10 retry attempts
	}

	// Apply any provided options
	if len(opts) > 0 {
		options = opts[0]
	}

	return &Client{
		url:           url,
		topics:        topics,
		options:       options,
		eventHandlers: make(map[string][]EventHandler),
		headers:       make(map[string]string),
		done:          make(chan struct{}),
	}
}

// SetHeader sets a custom HTTP header to be sent with each SSE connection.
// Must be called before Connect(). This is the only addition vs upstream.
func (c *Client) SetHeader(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.headers[key] = value
}

// addJitter adds +/- 2% jitter to the backoff duration
func addJitter(backoff time.Duration) time.Duration {
	// Add random jitter of +/- 2%
	jitterPercent := 0.02
	jitterRange := float64(backoff) * jitterPercent

	// Generate random jitter between -jitterRange and +jitterRange
	jitter := time.Duration(rand.Float64()*2*jitterRange - jitterRange)

	return backoff + jitter
}

// OnMessage sets the default message handler for events without a specific type
func (c *Client) OnMessage(handler EventHandler) {
	c.messageHandler = handler
}

// OnEvent sets a handler for a specific event type
func (c *Client) OnEvent(eventType string, handler EventHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.eventHandlers[eventType] == nil {
		c.eventHandlers[eventType] = make([]EventHandler, 0)
	}
	c.eventHandlers[eventType] = append(c.eventHandlers[eventType], handler)
}

// OnError sets the error handler
func (c *Client) OnError(handler ErrorHandler) {
	c.errorHandler = handler
}

// Connect establishes the SSE connection.
// Returns ErrClientClosed if the client has been disconnected via
// Disconnect() — the Client is single-use and cannot be restarted.
func (c *Client) Connect() error {
	// If done is already closed, the client was shut down — refuse
	// to reconnect to prevent goroutine/connection leaks.
	select {
	case <-c.done:
		return ErrClientClosed
	default:
	}

	c.setState(StateConnecting)

	conn := NewConnection(c.url, c.topics)
	c.lastEventIDMu.RLock()
	lastID := c.lastEventID
	c.lastEventIDMu.RUnlock()
	if lastID != "" {
		conn.SetLastEventID(lastID)
	}

	// Apply custom headers (the only addition vs upstream)
	c.mu.RLock()
	for key, value := range c.headers {
		conn.SetHeader(key, value)
	}
	c.mu.RUnlock()

	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()

	eventChan, err := conn.Connect()
	if err != nil {
		c.setState(StateDisconnected)
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.setState(StateConnected)

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.handleEvents(eventChan)
	}()

	return nil
}

// handleEvents processes events from the connection
func (c *Client) handleEvents(eventChan <-chan sse.Event) {
	for {
		select {
		case <-c.done:
			return
		case event, ok := <-eventChan:
			if !ok {
				// Connection closed
				c.handleDisconnection()
				return
			}

			c.handleEvent(event)
		}
	}
}

// handleEvent routes an event to the appropriate handler
func (c *Client) handleEvent(event sse.Event) {
	// Update last event ID for reconnection
	if event.ID != "" {
		c.lastEventIDMu.Lock()
		c.lastEventID = event.ID
		c.lastEventIDMu.Unlock()
	}

	// Handle heartbeat events
	if event.IsHeartbeat() {
		return
	}

	// Handle events with specific types
	if event.Type != "" && event.Type != "message" {
		c.mu.RLock()
		handlers := c.eventHandlers[event.Type]
		c.mu.RUnlock()

		for _, handler := range handlers {
			handler(event)
		}
	}

	// Handle default message handler
	if c.messageHandler != nil {
		c.messageHandler(event)
	}
}

// handleDisconnection handles connection loss and reconnection
func (c *Client) handleDisconnection() {
	c.setState(StateDisconnected)

	if !c.options.Reconnect {
		return
	}

	c.setState(StateReconnecting)

	// Exponential backoff reconnection
	backoff := c.options.Backoff
	retryCount := 0

	for {
		select {
		case <-c.done:
			return
		case <-time.After(addJitter(backoff)):
			retryCount++
			slog.Info("attempting to reconnect", "url", c.url, "backoff", backoff, "jitteredBackoff", addJitter(backoff), "attempt", retryCount)

			if err := c.Connect(); err != nil {
				if err == ErrClientClosed {
					// Client was disconnected via Disconnect() — stop reconnecting
					return
				}
				slog.Warn("reconnection failed", "error", err, "backoff", backoff, "attempt", retryCount)

				// Check if we've exceeded max retries
				if c.options.MaxRetries > 0 && retryCount >= c.options.MaxRetries {
					slog.Error("max reconnection attempts exceeded", "maxRetries", c.options.MaxRetries)
					c.setState(StateDisconnected)

					// Call error handler if set
					if c.errorHandler != nil {
						c.errorHandler(fmt.Errorf("failed to reconnect after %d attempts", c.options.MaxRetries))
					}
					return
				}

				// Increase backoff for next attempt
				backoff *= 2
				if backoff > c.options.MaxBackoff {
					backoff = c.options.MaxBackoff
				}
				continue
			}

			slog.Info("reconnection successful", "attempts", retryCount)
			// Reset reconnection state after a successful session
			retryCount = 0
			backoff = c.options.Backoff
			return
		}
	}
}

// Disconnect closes the connection
func (c *Client) Disconnect() {
	c.closeOnce.Do(func() {
		close(c.done)
	})

	c.connMu.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connMu.Unlock()

	c.setState(StateDisconnected)
}

// Wait blocks until the handleEvents goroutine has finished.
// Must be called after Disconnect().
func (c *Client) Wait() {
	c.wg.Wait()
}

// GetState returns the current connection state
func (c *Client) GetState() ConnectionState {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.state
}

// setState updates the connection state
func (c *Client) setState(state ConnectionState) {
	c.stateMu.Lock()
	c.state = state
	c.stateMu.Unlock()
}

// IsConnected returns true if the client is currently connected
func (c *Client) IsConnected() bool {
	return c.GetState() == StateConnected
}

// GetLastEventID returns the last event ID received
func (c *Client) GetLastEventID() string {
	c.lastEventIDMu.RLock()
	defer c.lastEventIDMu.RUnlock()
	return c.lastEventID
}
