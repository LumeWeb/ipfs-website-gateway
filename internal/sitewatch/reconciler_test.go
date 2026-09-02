package sitewatch

import (
	"context"
	"sync"
	"testing"
	"time"

	sdk "go.lumeweb.com/ipfs-sdk"
	"go.lumeweb.com/ipfs-website-gateway/internal/cache"
	"go.uber.org/zap"
)

func strPtr(s string) *string { return &s }

type fakeReconcilerAPI struct {
	mu      sync.Mutex
	calls   []string
	replies []*sdk.WebsiteChangesResponse
	err     error
}

func (f *fakeReconcilerAPI) ReconcileWebsiteChanges(_ context.Context, after string) (*sdk.WebsiteChangesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, after)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.replies) == 0 {
		return &sdk.WebsiteChangesResponse{}, nil
	}
	resp := f.replies[0]
	f.replies = f.replies[1:]
	return resp, nil
}

func (f *fakeReconcilerAPI) afters() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

type reconCache struct {
	mu          sync.Mutex
	invalidated []string
}

func (c *reconCache) Invalidate(domain string) {
	c.mu.Lock()
	c.invalidated = append(c.invalidated, domain)
	c.mu.Unlock()
}

func (c *reconCache) domains() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.invalidated...)
}

func waitReady(t *testing.T, r *Reconciler) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r.Ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("reconciler did not become ready in time")
}

func newTestReconciler(api *fakeReconcilerAPI, st *reconCache, prewarm ReconcileHandler) *Reconciler {
	return NewReconciler(api, st, cache.NewSSECursorStore(nil), prewarm, zap.NewNop())
}

func TestReconciler_ReconcileOnConnect(t *testing.T) {
	api := &fakeReconcilerAPI{
		replies: []*sdk.WebsiteChangesResponse{
			{
				Events: []sdk.WebsiteChangeEvent{
					{Id: 1, Domain: "a.com", EventType: sdk.WebsiteChangeEventPublished, Cid: strPtr("QmA")},
					{Id: 2, Domain: "b.com", EventType: sdk.WebsiteChangeEventRemoved},
				},
				HighWaterMark: 2,
			},
		},
	}
	store := &reconCache{}
	var prewarmed []string
	var prewarmMu sync.Mutex
	r := newTestReconciler(api, store, func(domain, cid string) {
		prewarmMu.Lock()
		prewarmed = append(prewarmed, domain+":"+cid)
		prewarmMu.Unlock()
	})

	// Start reporting connected (disconnected -> connected triggers reconcile).
	r.OnConnect(true)
	waitReady(t, r)

	if got := store.domains(); len(got) != 2 || got[0] != "a.com" || got[1] != "b.com" {
		t.Fatalf("invalidated = %v, want [a.com b.com]", got)
	}
	prewarmMu.Lock()
	defer prewarmMu.Unlock()
	if len(prewarmed) != 1 || prewarmed[0] != "a.com:QmA" {
		t.Fatalf("prewarmed = %v, want ['a.com:QmA']", prewarmed)
	}
	if got := api.afters(); len(got) != 1 || got[0] != "0" {
		t.Fatalf("reconcile cursor args = %v, want ['0']", got)
	}
}

func TestReconciler_NoReplayWhileConnected(t *testing.T) {
	api := &fakeReconcilerAPI{
		replies: []*sdk.WebsiteChangesResponse{
			{Events: []sdk.WebsiteChangeEvent{{Id: 1, Domain: "a.com", EventType: sdk.WebsiteChangeEventPublished, Cid: strPtr("QmA")}}, HighWaterMark: 1},
		},
	}
	store := &reconCache{}
	r := newTestReconciler(api, store, nil)

	r.OnConnect(true)
	waitReady(t, r)
	// Repeat connected reports (e.g. the stats poller) must not re-reconcile.
	r.OnConnect(true)
	time.Sleep(50 * time.Millisecond)

	if got := api.afters(); len(got) != 1 {
		t.Fatalf("reconcile calls = %v, want a single reconcile while connected", got)
	}
}

func TestReconciler_ReconcileAgainAfterDisconnect(t *testing.T) {
	api := &fakeReconcilerAPI{
		replies: []*sdk.WebsiteChangesResponse{
			{Events: []sdk.WebsiteChangeEvent{{Id: 1, Domain: "a.com", EventType: sdk.WebsiteChangeEventPublished, Cid: strPtr("QmA")}}, HighWaterMark: 1},
			{Events: []sdk.WebsiteChangeEvent{{Id: 2, Domain: "b.com", EventType: sdk.WebsiteChangeEventRemoved}}, HighWaterMark: 2},
		},
	}
	store := &reconCache{}
	r := newTestReconciler(api, store, nil)

	r.OnConnect(true)
	waitReady(t, r)

	// Disconnect then reconnect: reconciliation runs again from the persisted cursor.
	r.OnConnect(false)
	r.OnConnect(true)
	waitReady(t, r)

	if got := api.afters(); len(got) != 2 || got[1] != "1" {
		t.Fatalf("reconcile cursor args = %v, want cursor '1' on reconnect", got)
	}
	if got := store.domains(); len(got) != 2 {
		t.Fatalf("invalidated = %v, want 2 domains across both reconciles", got)
	}
}

func TestReconciler_TruncatedPagination(t *testing.T) {
	api := &fakeReconcilerAPI{
		replies: []*sdk.WebsiteChangesResponse{
			{Events: []sdk.WebsiteChangeEvent{{Id: 1, Domain: "a.com", EventType: sdk.WebsiteChangeEventPublished, Cid: strPtr("QmA")}}, HighWaterMark: 1, Truncated: true},
			{Events: []sdk.WebsiteChangeEvent{{Id: 2, Domain: "b.com", EventType: sdk.WebsiteChangeEventRemoved}}, HighWaterMark: 2},
		},
	}
	store := &reconCache{}
	r := newTestReconciler(api, store, nil)

	r.OnConnect(true)
	waitReady(t, r)

	if got := api.afters(); len(got) != 2 || got[0] != "0" || got[1] != "1" {
		t.Fatalf("reconcile cursor args = %v, want ['0' '1']", got)
	}
	if got := store.domains(); len(got) != 2 {
		t.Fatalf("invalidated = %v, want 2 domains", got)
	}
}

func TestReconciler_NotReadyOnFailure(t *testing.T) {
	api := &fakeReconcilerAPI{err: context.DeadlineExceeded}
	store := &reconCache{}
	r := newTestReconciler(api, store, nil)

	r.OnConnect(true)
	time.Sleep(50 * time.Millisecond)

	if r.Ready() {
		t.Fatal("reconciler should not be ready after a failed reconcile")
	}
	if got := store.domains(); len(got) != 0 {
		t.Fatalf("invalidated = %v, want none on failure", got)
	}
}
