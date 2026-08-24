package sitewatch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
)

type fakeAPI struct {
	mu       sync.Mutex
	perSite  map[string]*types.GatewayWebsiteResponse
	perError map[string]error
	calls    map[string]int
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		perSite:  make(map[string]*types.GatewayWebsiteResponse),
		perError: make(map[string]error),
		calls:    make(map[string]int),
	}
}

func (f *fakeAPI) setActive(domain string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.perSite[domain] = &types.GatewayWebsiteResponse{Status: types.StatusActive}
}

func (f *fakeAPI) setBroken(domain string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.perSite[domain] = &types.GatewayWebsiteResponse{Status: types.StatusBroken}
}

func (f *fakeAPI) setError(domain string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.perError[domain] = err
}

func (f *fakeAPI) callCount(domain string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[domain]
}

func (f *fakeAPI) GetWebsite(_ context.Context, domain string) (*types.GatewayWebsiteResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[domain]++
	if err, ok := f.perError[domain]; ok {
		return nil, err
	}
	return f.perSite[domain], nil
}

type fakeCache struct {
	mu          sync.Mutex
	invalidated []string
}

func (c *fakeCache) Invalidate(domain string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.invalidated = append(c.invalidated, domain)
}

func (c *fakeCache) invalidatedDomains() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.invalidated))
	copy(out, c.invalidated)
	return out
}

type fakeConn struct {
	connected atomic.Bool
}

func (c *fakeConn) IsConnected() bool { return c.connected.Load() }

func newTestWatcher(api *fakeAPI, cache *fakeCache, conn *fakeConn, onRecover RecoverHandler) *BrokenWatcher {
	return NewBrokenWatcher(api, cache, conn, time.Hour, onRecover, nil)
}

func contains(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}

func TestPollRecoversActiveSite(t *testing.T) {
	api := newFakeAPI()
	api.setActive("example.com")
	cache := &fakeCache{}
	conn := &fakeConn{}
	conn.connected.Store(false)

	var recovered []string
	w := newTestWatcher(api, cache, conn, func(domain string) {
		recovered = append(recovered, domain)
	})
	w.MarkBroken("example.com")

	w.poll()

	if !contains(cache.invalidatedDomains(), "example.com") {
		t.Fatalf("expected cache to be invalidated for example.com, got %v", cache.invalidatedDomains())
	}
	if len(recovered) != 1 || recovered[0] != "example.com" {
		t.Fatalf("expected recover callback for example.com, got %v", recovered)
	}

	w.mu.Lock()
	_, stillTracked := w.broken["example.com"]
	w.mu.Unlock()
	if stillTracked {
		t.Fatal("expected example.com to be removed from the watch set after recovery")
	}
}

func TestPollKeepsBrokenSite(t *testing.T) {
	api := newFakeAPI()
	api.setBroken("example.com")
	cache := &fakeCache{}
	conn := &fakeConn{}
	conn.connected.Store(false)

	w := newTestWatcher(api, cache, conn, nil)
	w.MarkBroken("example.com")

	w.poll()

	if len(cache.invalidatedDomains()) != 0 {
		t.Fatalf("did not expect cache invalidation while site still broken, got %v", cache.invalidatedDomains())
	}

	w.mu.Lock()
	_, stillTracked := w.broken["example.com"]
	w.mu.Unlock()
	if !stillTracked {
		t.Fatal("expected example.com to remain in the watch set while broken")
	}
}

func TestPollIgnoresAPIError(t *testing.T) {
	api := newFakeAPI()
	api.setError("example.com", errors.New("boom"))
	cache := &fakeCache{}
	conn := &fakeConn{}
	conn.connected.Store(false)

	w := newTestWatcher(api, cache, conn, nil)
	w.MarkBroken("example.com")

	w.poll()

	if len(cache.invalidatedDomains()) != 0 {
		t.Fatalf("did not expect cache invalidation on API error")
	}

	w.mu.Lock()
	_, stillTracked := w.broken["example.com"]
	w.mu.Unlock()
	if !stillTracked {
		t.Fatal("expected site to remain in the watch set on transient API error")
	}
}

func TestClearEmptiesSet(t *testing.T) {
	api := newFakeAPI()
	cache := &fakeCache{}
	conn := &fakeConn{}
	conn.connected.Store(true)

	w := newTestWatcher(api, cache, conn, nil)
	w.MarkBroken("example.com")
	w.MarkBroken("other.com")

	if api.callCount("example.com") != 0 {
		t.Fatal("expected no polls before clear")
	}

	w.clear()

	w.mu.Lock()
	n := len(w.broken)
	w.mu.Unlock()
	if n != 0 {
		t.Fatalf("expected watch set to be cleared, got %d entries", n)
	}
}

func TestLoopRecoversAfterReconnect(t *testing.T) {
	api := newFakeAPI()
	api.setBroken("example.com")
	cache := &fakeCache{}
	conn := &fakeConn{}
	conn.connected.Store(false)

	var recovered atomic.Int64
	// Very short interval to exercise the real polling loop.
	w := NewBrokenWatcher(api, cache, conn, 10*time.Millisecond, func(domain string) {
		recovered.Add(1)
	}, nil)
	w.MarkBroken("example.com")
	w.Start()

	// Site recovers: switch the API response and mark SSE connected. On the
	// next tick the loop sees the connection is up and clears the set.
	api.setActive("example.com")
	conn.connected.Store(true)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		n := len(w.broken)
		w.mu.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	w.Stop()

	w.mu.Lock()
	n := len(w.broken)
	w.mu.Unlock()
	if n != 0 {
		t.Fatalf("expected watch set to be cleared after reconnect, got %d entries", n)
	}
	if recovered.Load() != 0 {
		t.Fatal("did not expect recover callback when reconnect cleared the set")
	}
}

func TestMarkBrokenIgnoresEmpty(t *testing.T) {
	api := newFakeAPI()
	cache := &fakeCache{}
	conn := &fakeConn{}

	w := newTestWatcher(api, cache, conn, nil)
	w.MarkBroken("")

	w.mu.Lock()
	n := len(w.broken)
	w.mu.Unlock()
	if n != 0 {
		t.Fatalf("expected empty domain to be ignored, got %d entries", n)
	}
}

func TestNewBrokenWatcherDefaultsInterval(t *testing.T) {
	api := newFakeAPI()
	cache := &fakeCache{}
	conn := &fakeConn{}

	w := NewBrokenWatcher(api, cache, conn, 0, nil, nil)
	if w.interval != 30*time.Second {
		t.Fatalf("expected default interval of 30s, got %v", w.interval)
	}
	if w.api != api {
		t.Fatal("expected api client to be stored")
	}
}

var _ APIClient = (*fakeAPI)(nil)
var _ StatusCache = (*fakeCache)(nil)
var _ ConnState = (*fakeConn)(nil)
