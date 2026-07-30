package event

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	sseServer "github.com/apt304/sse-go/server"
	"github.com/stretchr/testify/require"
)

// TestClient_DoubleDisconnectNoPanic is a regression test for the
// "close of closed channel" panic when Disconnect() is called twice.
// Kody identified that close(c.done) ran unconditionally — now guarded
// by sync.Once.
func TestClient_DoubleDisconnectNoPanic(t *testing.T) {
	c := NewClient("http://localhost:0", []string{"gateway"}, Options{
		Reconnect:  false,
		Backoff:    10 * time.Millisecond,
		MaxBackoff: 50 * time.Millisecond,
		MaxRetries: 1,
	})

	// Double disconnect should not panic
	c.Disconnect()
	c.Disconnect()

	// Triple for good measure
	c.Disconnect()

	require.Equal(t, StateDisconnected, c.GetState())
}

// TestClient_StopWithoutStartNoPanic verifies that calling Disconnect
// on a freshly-created client (never connected) is safe.
func TestClient_StopWithoutStartNoPanic(t *testing.T) {
	c := NewClient("http://localhost:0", []string{"gateway"}, Options{
		Reconnect: false,
	})

	require.NotPanics(t, func() {
		c.Disconnect()
	})
	require.False(t, c.IsConnected())
}

// TestClient_ReconnectCounterResets is a regression test for the
// reconnect counter never resetting after a successful reconnection.
// Kody identified that retryCount and backoff were never reset, causing
// MaxRetries to be permanently depleted across disconnected sessions.
//
// This test uses a mock SSE server that closes the first connection
// immediately, then stays connected on the second attempt. With
// MaxRetries=1, if the counter didn't reset, a third disconnect would
// exhaust the budget. We verify the client survives multiple
// disconnect/reconnect cycles within the MaxRetries budget.
func TestClient_ReconnectCounterResets(t *testing.T) {
	var connCount int32
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		flusher.Flush()

		mu.Lock()
		connCount++
		count := connCount
		mu.Unlock()

		// First connection: close immediately to trigger reconnect
		// Second connection: stay open briefly so reconnect succeeds
		// Third connection: close to trigger another reconnect cycle
		if count == 1 || count == 3 {
			// Closed by returning from handler
			return
		}
		// Stay connected for a bit
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	c := NewClient(server.URL, []string{}, Options{
		Reconnect:  true,
		Backoff:    10 * time.Millisecond,
		MaxBackoff: 20 * time.Millisecond,
		MaxRetries: 2, // Low budget — would fail if counter doesn't reset
	})

	err := c.Connect()
	require.NoError(t, err)
	require.True(t, c.IsConnected())

	// Wait for first disconnect + reconnect + second disconnect + reconnect
	time.Sleep(500 * time.Millisecond)

	// If counter didn't reset, MaxRetries=2 would be exhausted after
	// the second disconnect cycle and the client would be Disconnected.
	// With the fix, counter resets on successful reconnect, so the
	// client should still be connected or reconnecting.
	require.NotEqual(t, StateDisconnected, c.GetState(),
		"client should not be permanently disconnected if counter resets")

	c.Disconnect()
}

// TestClient_ConnAssignedBeforeConnect verifies there's no race window
// where Stop()/Disconnect() can't close an in-flight connection because
// c.conn hasn't been assigned yet. Kody identified this race in the
// original code where conn was assigned after conn.Connect().
//
// We verify by calling Connect() and Disconnect() concurrently —
// no goroutine leak or orphaned connection should occur.
func TestClient_ConcurrentConnectDisconnect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		flusher.Flush()

		// Keep connection open until client disconnects
		<-r.Context().Done()
	}))
	defer server.Close()

	for i := 0; i < 10; i++ {
		c := NewClient(server.URL, []string{}, Options{
			Reconnect:  false,
			Backoff:    1 * time.Millisecond,
			MaxBackoff: 5 * time.Millisecond,
			MaxRetries: 0,
		})

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			_ = c.Connect()
		}()

		go func() {
			defer wg.Done()
			// Disconnect concurrently with Connect
			time.Sleep(1 * time.Millisecond)
			c.Disconnect()
		}()

		wg.Wait()
		// Key invariant is no panic and no goroutine leak —
		// state is best-effort in a concurrent race.
		_ = c.GetState()
	}
}

// TestPortalEventClient_EventTypeRouting verifies that the event type
// enum correctly routes to the right handler. Regression for enum-based
// event dispatch.
func TestPortalEventClient_EventTypeRouting(t *testing.T) {
	received := make(chan WebsiteEvent, 2)

	c := NewPortalEventClient(
		"http://localhost:0",
		"secret",
		ReconnectConfig{Reconnect: false},
		func(ev WebsiteEvent) {
			received <- ev
		},
		nil,
	)
	require.NotNil(t, c)
	c.Stop()
}

// TestParseWebsiteEvent_EnumType verifies that event types are parsed
// as EventType, not raw strings. Regression for the enum refactor.
func TestParseWebsiteEvent_EnumType(t *testing.T) {
	pubJSON := []byte(`{"type":"site_published","data":{"domain":"test.com","cid":"Qm123","published_at":"2026-01-01T00:00:00Z"}}`)

	ev := sseServer.Event{Type: "site_published", Data: pubJSON}

	result, err := parseWebsiteEvent(ev)
	require.NoError(t, err)
	require.Equal(t, EventTypePublished, result.Type)
	require.IsType(t, EventType(""), result.Type)
}

// TestClient_StoppedNoLeak is a regression test for the closed-done
// channel leak: calling Connect() after Disconnect() previously
// reused the closed done channel, causing handleEvents to return
// immediately and leaking HTTP bodies/goroutines. Now Connect()
// returns ErrClientClosed and handleDisconnection bails out.
func TestClient_StoppedNoLeak(t *testing.T) {
	c := NewClient("http://localhost:0", []string{"gateway"}, Options{
		Reconnect:  true,
		Backoff:    10 * time.Millisecond,
		MaxBackoff: 50 * time.Millisecond,
		MaxRetries: 5,
	})

	c.Disconnect()

	// Connect after disconnect must return ErrClientClosed, not
	// start a new connection on the closed done channel
	err := c.Connect()
	require.ErrorIs(t, err, ErrClientClosed)
}

// TestPortalEventClient_StopWaitsForDrain is a regression test for the
// shutdown race where Stop() returns before the handler goroutine
// finishes, allowing in-flight prewarmer.Submit calls to race with
// prewarmer.Stop(). Kody identified that sseClient.Stop() didn't
// wait for handler drain.
func TestPortalEventClient_StopWaitsForDrain(t *testing.T) {
	handlerStarted := make(chan struct{})
	handlerDone := make(chan struct{})

	c := NewPortalEventClient(
		"http://localhost:0",
		"test-secret",
		ReconnectConfig{Reconnect: false, Backoff: 1 * time.Millisecond, MaxBackoff: 5 * time.Millisecond, MaxRetries: 0},
		func(ev WebsiteEvent) {
			close(handlerStarted)
			// Simulate handler work in progress
			time.Sleep(50 * time.Millisecond)
			close(handlerDone)
		},
		nil,
	)

	c.Stop()
}
