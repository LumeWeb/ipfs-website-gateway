package event

import (
	"encoding/json"
	"testing"

	sseServer "github.com/apt304/sse-go/server"
	"github.com/stretchr/testify/require"
)

func TestParseWebsiteEvent_Published(t *testing.T) {
	pub := WebsitePublishedEvent{
		Domain:      "example.com",
		CID:         "QmTest",
		PublishedAt: "2026-07-30T10:00:00Z",
	}
	pubJSON, _ := json.Marshal(pub)

	wrapper := SSEEvent{
		Type: EventTypePublished,
		Data: pubJSON,
	}
	wrapperJSON, _ := json.Marshal(wrapper)

	ev := sseServer.Event{Type: "site_published", Data: wrapperJSON}

	result, err := parseWebsiteEvent(ev)
	require.NoError(t, err)
	require.Equal(t, EventTypePublished, result.Type)
	require.NotNil(t, result.Published)
	require.Equal(t, "example.com", result.Published.Domain)
	require.Equal(t, "QmTest", result.Published.CID)
}

func TestParseWebsiteEvent_Removed(t *testing.T) {
	rem := WebsiteRemovedEvent{
		Domain:    "example.com",
		RemovedAt: "2026-07-30T10:00:00Z",
	}
	remJSON, _ := json.Marshal(rem)

	wrapper := SSEEvent{
		Type: EventTypeRemoved,
		Data: remJSON,
	}
	wrapperJSON, _ := json.Marshal(wrapper)

	ev := sseServer.Event{Type: "site_removed", Data: wrapperJSON}

	result, err := parseWebsiteEvent(ev)
	require.NoError(t, err)
	require.Equal(t, EventTypeRemoved, result.Type)
	require.NotNil(t, result.Removed)
	require.Equal(t, "example.com", result.Removed.Domain)
}

func TestParseWebsiteEvent_Heartbeat(t *testing.T) {
	// Heartbeat events have empty type and data ":heartbeat"
	ev := sseServer.Event{Type: "", Data: []byte(":heartbeat")}

	// Heartbeats are filtered before parseWebsiteEvent is called,
	// but parse should still handle gracefully
	_, err := parseWebsiteEvent(ev)
	require.Error(t, err) // empty data won't unmarshal as SSEEvent
}

func TestParseWebsiteEvent_UnknownType(t *testing.T) {
	wrapper := SSEEvent{
		Type: EventType("unknown_event"),
		Data: json.RawMessage(`{}`),
	}
	wrapperJSON, _ := json.Marshal(wrapper)

	ev := sseServer.Event{Type: "unknown_event", Data: wrapperJSON}

	result, err := parseWebsiteEvent(ev)
	require.NoError(t, err)
	require.Equal(t, EventType("unknown_event"), result.Type)
	require.Nil(t, result.Published)
	require.Nil(t, result.Removed)
}

func TestParseWebsiteEvent_InvalidJSON(t *testing.T) {
	ev := sseServer.Event{Type: "site_published", Data: []byte("not json")}

	_, err := parseWebsiteEvent(ev)
	require.Error(t, err)
}

func TestParseWebsiteEvent_InvalidDataPayload(t *testing.T) {
	wrapper := SSEEvent{
		Type: "site_published",
		Data: json.RawMessage(`invalid`),
	}
	wrapperJSON, _ := json.Marshal(wrapper)

	ev := sseServer.Event{Type: "site_published", Data: wrapperJSON}

	_, err := parseWebsiteEvent(ev)
	require.Error(t, err)
}

func TestNewPortalEventClient(t *testing.T) {
	client := NewPortalEventClient(
		"http://localhost:8080/internal/websites/events",
		"test-secret",
		ReconnectConfig{
			Reconnect:  true,
			Backoff:    100_000_000, // 100ms
			MaxBackoff: 500_000_000, // 500ms
			MaxRetries: 3,
		},
		nil,
		nil, // logger nil-safe for construction
	)
	require.NotNil(t, client)
	require.False(t, client.IsConnected())
}

func TestPortalEventClient_StopWithoutStart(t *testing.T) {
	client := NewPortalEventClient(
		"http://localhost:8080/internal/websites/events",
		"test-secret",
		ReconnectConfig{Reconnect: false},
		nil,
		nil,
	)
	// Stop should be safe to call even if never started
	client.Stop()
	require.False(t, client.IsConnected())
}
