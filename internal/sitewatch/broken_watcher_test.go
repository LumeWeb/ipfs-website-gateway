package sitewatch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ipfs "go.lumeweb.com/ipfs-sdk"
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
	delete(f.perError, domain)
}

func (f *fakeAPI) setBroken(domain string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.perSite[domain] = &types.GatewayWebsiteResponse{Status: types.StatusBroken}
	delete(f.perError, domain)
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
	return NewBrokenWatcher(api, cache, conn, time.Hour, 3, onRecover, nil)
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
	w := newTestWatcher(api, cache, conn, func(domain, targetHash string) {
		recovered = append(recovered, domain)
	})
	w.MarkBroken("example.com")

	w.poll()
	w.recoverWG.Wait() // recover callback runs asynchronously

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

func TestPollDoesNotDropOnSingle404(t *testing.T) {
	// A single 404 can be transient (content not yet pinned/replicated), so
	// the site must stay in the watch set until the 404 is confirmed.
	api := newFakeAPI()
	api.setError("gone.example.com", ipfs.ErrNotFound)
	cache := &fakeCache{}
	conn := &fakeConn{}
	conn.connected.Store(false)

	w := newTestWatcher(api, cache, conn, nil) // deletedConfirmCount = 3
	w.MarkBroken("gone.example.com")

	w.poll()

	w.mu.Lock()
	_, stillTracked := w.broken["gone.example.com"]
	count := w.notFound["gone.example.com"]
	w.mu.Unlock()

	if !stillTracked {
		t.Fatal("expected site to remain in the watch set after a single 404")
	}
	if count != 1 {
		t.Fatalf("expected notFound counter of 1, got %d", count)
	}
}

func TestPollDropsAfterConfirmed404s(t *testing.T) {
	api := newFakeAPI()
	api.setError("gone.example.com", ipfs.ErrNotFound)
	api.setError("broken.example.com", ipfs.ErrGone)
	cache := &fakeCache{}
	conn := &fakeConn{}
	conn.connected.Store(false)

	w := newTestWatcher(api, cache, conn, nil) // deletedConfirmCount = 3
	w.MarkBroken("gone.example.com")
	w.MarkBroken("broken.example.com")

	// Two 404s: not yet confirmed.
	w.poll()
	w.poll()
	w.mu.Lock()
	_, stillTracked := w.broken["gone.example.com"]
	w.mu.Unlock()
	if !stillTracked {
		t.Fatal("expected site to remain after two 404s (below threshold)")
	}

	// Third 404 confirms deletion.
	w.poll()
	w.mu.Lock()
	_, stillGone := w.broken["gone.example.com"]
	_, stillBroken := w.broken["broken.example.com"]
	w.mu.Unlock()

	if stillGone {
		t.Fatal("expected deleted (404) site to be dropped after confirmation")
	}
	if !stillBroken {
		t.Fatal("expected broken (410) site to remain in the watch set")
	}
	if len(cache.invalidatedDomains()) != 0 {
		t.Fatalf("did not expect cache invalidation for deleted site, got %v", cache.invalidatedDomains())
	}
}

func TestReconfirmDeletedNeedsFullCount(t *testing.T) {
	// After a site is dropped for confirmed 404, it is re-marked broken on the
	// next broken request. A single stale 404 must not immediately drop it
	// again; the full confirmation count is required fresh each time.
	api := newFakeAPI()
	api.setError("gone.example.com", ipfs.ErrNotFound)
	cache := &fakeCache{}
	conn := &fakeConn{}
	conn.connected.Store(false)

	w := newTestWatcher(api, cache, conn, nil) // deletedConfirmCount = 3
	w.MarkBroken("gone.example.com")

	w.poll() // 404 #1
	w.poll() // 404 #2
	w.poll() // 404 #3 -> confirmed, dropped

	w.mu.Lock()
	_, dropped := w.broken["gone.example.com"]
	w.mu.Unlock()
	if dropped {
		t.Fatal("expected site to be dropped after confirmed 404s")
	}

	// Re-marked (as the gateway would on the next broken request) and hit with
	// a single 404: the stale counter must not be reused.
	w.MarkBroken("gone.example.com")
	w.poll() // 404 #1 (fresh streak)

	w.mu.Lock()
	count := w.notFound["gone.example.com"]
	_, stillTracked := w.broken["gone.example.com"]
	w.mu.Unlock()

	if count != 1 {
		t.Fatalf("expected fresh notFound streak of 1 after re-mark, got %d", count)
	}
	if !stillTracked {
		t.Fatal("expected re-marked site to remain tracked after a single 404")
	}
}

func TestPollResetsNotFoundStreakOnSuccess(t *testing.T) {
	// A successful (site exists, still broken) response between 404s resets
	// the confirmation streak so the site is not dropped prematurely.
	api := newFakeAPI()
	api.setError("gone.example.com", ipfs.ErrNotFound)
	cache := &fakeCache{}
	conn := &fakeConn{}
	conn.connected.Store(false)

	w := newTestWatcher(api, cache, conn, nil) // deletedConfirmCount = 3
	w.MarkBroken("gone.example.com")

	w.poll()                          // 404 #1
	w.poll()                          // 404 #2
	api.setBroken("gone.example.com") // now exists, just broken
	w.poll()                          // success resets the streak

	w.mu.Lock()
	count := w.notFound["gone.example.com"]
	_, stillTracked := w.broken["gone.example.com"]
	w.mu.Unlock()

	if count != 0 {
		t.Fatalf("expected notFound streak to reset on success, got %d", count)
	}
	if !stillTracked {
		t.Fatal("expected site to remain in the watch set (still broken)")
	}

	// Deleted again: must start over from 0.
	api.setError("gone.example.com", ipfs.ErrNotFound)
	w.poll() // 404 #1 after reset
	w.mu.Lock()
	count = w.notFound["gone.example.com"]
	w.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected notFound streak to restart at 1, got %d", count)
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
	w := NewBrokenWatcher(api, cache, conn, 10*time.Millisecond, 3, func(domain, targetHash string) {
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

func TestClearHeldUntilReconciliation(t *testing.T) {
	api := newFakeAPI()
	cache := &fakeCache{}
	conn := &fakeConn{}
	conn.connected.Store(false)

	var reconcileReady atomic.Bool
	reconcileReady.Store(false)

	w := NewBrokenWatcher(api, cache, conn, 10*time.Millisecond, 3, nil, nil)
	w.SetReconciliationReady(reconcileReady.Load)
	w.MarkBroken("example.com")
	w.Start()
	defer w.Stop()

	// Connected but reconciliation not yet complete: the watch set must be held.
	conn.connected.Store(true)
	time.Sleep(50 * time.Millisecond)

	w.mu.Lock()
	n := len(w.broken)
	w.mu.Unlock()
	if n != 1 {
		t.Fatalf("expected watch set held (1 entry) while reconciliation pending, got %d", n)
	}

	// Reconciliation completes: the next tick clears the set.
	reconcileReady.Store(true)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		n = len(w.broken)
		w.mu.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n != 0 {
		t.Fatalf("expected watch set cleared after reconciliation, got %d entries", n)
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

func TestNewBrokenWatcherDefaults(t *testing.T) {
	api := newFakeAPI()
	cache := &fakeCache{}
	conn := &fakeConn{}

	w := NewBrokenWatcher(api, cache, conn, 0, 0, nil, nil)
	if w.interval != 30*time.Second {
		t.Fatalf("expected default interval of 30s, got %v", w.interval)
	}
	if w.deletedConfirmCount != 3 {
		t.Fatalf("expected default deletedConfirmCount of 3, got %d", w.deletedConfirmCount)
	}
	if w.api != api {
		t.Fatal("expected api client to be stored")
	}
}

var _ APIClient = (*fakeAPI)(nil)
var _ StatusCache = (*fakeCache)(nil)
var _ ConnState = (*fakeConn)(nil)
