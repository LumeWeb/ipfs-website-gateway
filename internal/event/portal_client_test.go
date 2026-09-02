package event

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdk "go.lumeweb.com/ipfs-sdk"
)

func TestNewPortalEventClient(t *testing.T) {
	client, err := NewPortalEventClient(
		"http://localhost:8080",
		"test-secret",
		ReconnectConfig{
			Reconnect:  true,
			Backoff:    100 * time.Millisecond,
			MaxBackoff: 500 * time.Millisecond,
			MaxRetries: 3,
		},
		nil,
		nil, // onConnected optional
		nil, // logger nil-safe for construction
	)
	require.NoError(t, err)
	require.NotNil(t, client)
	require.False(t, client.IsConnected())
}

func TestNewPortalEventClient_EmptyBaseURL(t *testing.T) {
	_, err := NewPortalEventClient("", "test-secret", ReconnectConfig{}, nil, nil, nil)
	require.Error(t, err)
}

func TestPortalEventClient_StopWithoutStart(t *testing.T) {
	client, err := NewPortalEventClient(
		"http://localhost:8080",
		"test-secret",
		ReconnectConfig{Reconnect: false},
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)

	// Stop must be safe even if never started.
	client.Stop()
	client.Stop()
	require.False(t, client.IsConnected())
}

// TestPortalEventClient_StreamsLifecycleEvents verifies the SDK-backed client
// authenticates with the gateway secret and dispatches parsed lifecycle events
// through the wrapper.
func TestPortalEventClient_StreamsLifecycleEvents(t *testing.T) {
	var mu sync.Mutex
	var gotSecret string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotSecret = r.Header.Get("X-Gateway-Secret")
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		fmt.Fprint(w,
			"event: site_published\n"+
				"id: 1\n"+
				"data: {\"type\":\"site_published\",\"data\":{\"domain\":\"example.com\",\"cid\":\"QmPublish\",\"published_at\":\"2026-09-01T00:00:00Z\"}}\n\n"+
				"event: site_removed\n"+
				"id: 2\n"+
				"data: {\"type\":\"site_removed\",\"data\":{\"domain\":\"example.org\",\"removed_at\":\"2026-09-02T00:00:00Z\"}}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Keep the stream open until the client disconnects.
		<-r.Context().Done()
	}))
	defer srv.Close()

	events := make(chan sdk.WebsiteEvent, 2)
	client, err := NewPortalEventClient(
		srv.URL,
		"s3cr3t",
		ReconnectConfig{Reconnect: false},
		func(ev sdk.WebsiteEvent) {
			events <- ev
		},
		nil,
		nil,
	)
	require.NoError(t, err)

	client.Start()
	defer client.Stop()

	// Published event.
	select {
	case ev := <-events:
		require.Equal(t, sdk.WebsiteEventPublished, ev.Type)
		require.NotNil(t, ev.Published)
		require.Equal(t, "example.com", ev.Published.Domain)
		require.Equal(t, "QmPublish", ev.Published.CID)
		require.Nil(t, ev.Removed)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for published event")
	}

	// Removed event.
	select {
	case ev := <-events:
		require.Equal(t, sdk.WebsiteEventRemoved, ev.Type)
		require.NotNil(t, ev.Removed)
		require.Equal(t, "example.org", ev.Removed.Domain)
		require.Nil(t, ev.Published)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for removed event")
	}

	require.Equal(t, "2", client.LastEventID())

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "s3cr3t", gotSecret)
}
