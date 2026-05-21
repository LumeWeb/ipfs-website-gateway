package namesys

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ipfs/boxo/namesys"
	"github.com/ipfs/boxo/path"
	ci "github.com/libp2p/go-libp2p/core/crypto"
	"go.uber.org/zap"
)

var errTestResolve = errors.New("resolve failed")

type mockNameSystem struct {
	resolveErr  error
	resolveFn   func(ctx context.Context, p path.Path, opts ...namesys.ResolveOption) (namesys.Result, error)
	resolveCall atomic.Int32
	publishErr  error
}

func (m *mockNameSystem) Resolve(ctx context.Context, p path.Path, opts ...namesys.ResolveOption) (namesys.Result, error) {
	m.resolveCall.Add(1)
	if m.resolveFn != nil {
		return m.resolveFn(ctx, p, opts...)
	}
	if m.resolveErr != nil {
		return namesys.Result{}, m.resolveErr
	}
	return namesys.Result{Path: p}, nil
}

func (m *mockNameSystem) ResolveAsync(ctx context.Context, p path.Path, opts ...namesys.ResolveOption) <-chan namesys.AsyncResult {
	ch := make(chan namesys.AsyncResult, 1)
	result, err := m.Resolve(ctx, p, opts...)
	if err != nil {
		ch <- namesys.AsyncResult{Err: err}
	} else {
		ch <- namesys.AsyncResult{Path: result.Path, TTL: result.TTL, LastMod: result.LastMod}
	}
	close(ch)
	return ch
}

func (m *mockNameSystem) Publish(ctx context.Context, sk ci.PrivKey, value path.Path, opts ...namesys.PublishOption) error {
	return m.publishErr
}

func newPath(t *testing.T, s string) path.Path {
	t.Helper()
	p, err := path.NewPath(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func newTestStore(t *testing.T) *IPNSStore {
	t.Helper()
	store, err := NewIPNSStore(t.TempDir(), 5*time.Minute, 30*time.Second, 128, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestResolve_CacheHit_ReturnsFresh(t *testing.T) {
	mock := &mockNameSystem{}
	store := newTestStore(t)
	sut := NewStaleWhileRevalidateNameSystem(mock, store, 2, zap.NewNop())

	p := newPath(t, "/ipns/example.com")
	result, err := sut.Resolve(context.Background(), p)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Path.String() != p.String() {
		t.Errorf("expected path %s, got %s", p.String(), result.Path.String())
	}
	sut.Stop()
}

func TestResolve_StaleReturnedWhileRevalidating(t *testing.T) {
	p := newPath(t, "/ipns/example.com")
	callCount := int32(0)

	mock := &mockNameSystem{
		resolveFn: func(ctx context.Context, req path.Path, opts ...namesys.ResolveOption) (namesys.Result, error) {
			n := atomic.AddInt32(&callCount, 1)
			if n == 1 {
				return namesys.Result{Path: p}, nil
			}
			return namesys.Result{}, errTestResolve
		},
	}

	store := newTestStore(t)
	sut := NewStaleWhileRevalidateNameSystem(mock, store, 2, zap.NewNop())

	_, err := sut.Resolve(context.Background(), p)
	if err != nil {
		t.Fatalf("first resolve: expected no error, got %v", err)
	}

	result, err := sut.Resolve(context.Background(), p)
	if err != nil {
		t.Fatalf("second resolve (stale): expected no error, got %v", err)
	}
	if result.Path.String() != p.String() {
		t.Errorf("stale: expected path %s, got %s", p.String(), result.Path.String())
	}
	sut.Stop()
}

func TestResolve_StaleExpired_ReturnsError(t *testing.T) {
	p := newPath(t, "/ipns/example.com")

	mock := &mockNameSystem{
		resolveFn: func(ctx context.Context, req path.Path, opts ...namesys.ResolveOption) (namesys.Result, error) {
			return namesys.Result{}, errTestResolve
		},
	}

	store, err := NewIPNSStore(t.TempDir(), 1*time.Nanosecond, 30*time.Second, 128, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	sut := NewStaleWhileRevalidateNameSystem(mock, store, 2, zap.NewNop())

	store.PutStale(p.String(), staleEntry{
		result:   namesys.Result{Path: p},
		cachedAt: time.Now().Add(-time.Second),
	})

	_, err = sut.Resolve(context.Background(), p)
	if !errors.Is(err, errTestResolve) {
		t.Fatalf("expected errTestResolve, got %v", err)
	}
	sut.Stop()
}

func TestResolve_NoStale_NoCache_ReturnsError(t *testing.T) {
	mock := &mockNameSystem{resolveErr: errTestResolve}
	store := newTestStore(t)
	sut := NewStaleWhileRevalidateNameSystem(mock, store, 2, zap.NewNop())

	p := newPath(t, "/ipns/example.com")
	_, err := sut.Resolve(context.Background(), p)
	if !errors.Is(err, errTestResolve) {
		t.Fatalf("expected errTestResolve, got %v", err)
	}
	sut.Stop()
}

func TestResolveAsync_StaleReturnedWhileRevalidating(t *testing.T) {
	p := newPath(t, "/ipns/example.com")

	mock := &mockNameSystem{
		resolveFn: func(ctx context.Context, req path.Path, opts ...namesys.ResolveOption) (namesys.Result, error) {
			return namesys.Result{}, errTestResolve
		},
	}

	store := newTestStore(t)
	sut := NewStaleWhileRevalidateNameSystem(mock, store, 2, zap.NewNop())

	store.PutStale(p.String(), staleEntry{
		result:   namesys.Result{Path: p},
		cachedAt: time.Now(),
	})

	ch := sut.ResolveAsync(context.Background(), p)
	res := <-ch
	if res.Err != nil {
		t.Fatalf("expected no error from async stale, got %v", res.Err)
	}
	if res.Path.String() != p.String() {
		t.Errorf("async stale: expected path %s, got %s", p.String(), res.Path.String())
	}
	sut.Stop()
}

func TestRevalidate_DeduplicatesInFlight(t *testing.T) {
	p := newPath(t, "/ipns/example.com")
	resolveCh := make(chan struct{})

	mock := &mockNameSystem{
		resolveFn: func(ctx context.Context, req path.Path, opts ...namesys.ResolveOption) (namesys.Result, error) {
			<-resolveCh
			return namesys.Result{Path: p}, nil
		},
	}

	store := newTestStore(t)
	sut := NewStaleWhileRevalidateNameSystem(mock, store, 2, zap.NewNop())

	store.PutStale(p.String(), staleEntry{
		result:   namesys.Result{Path: p},
		cachedAt: time.Now(),
	})

	sut.revalidate(p, nil)
	sut.revalidate(p, nil)
	sut.revalidate(p, nil)

	close(resolveCh)
	sut.Stop()

	if mock.resolveCall.Load() != 1 {
		t.Errorf("expected 1 revalidation call, got %d", mock.resolveCall.Load())
	}
}

func TestStop_DrainsPool(t *testing.T) {
	mock := &mockNameSystem{}
	store := newTestStore(t)
	sut := NewStaleWhileRevalidateNameSystem(mock, store, 2, zap.NewNop())
	sut.Stop()
}

func TestPublish_DelegatesToInner(t *testing.T) {
	publishErr := errors.New("publish failed")
	mock := &mockNameSystem{publishErr: publishErr}
	store := newTestStore(t)
	sut := NewStaleWhileRevalidateNameSystem(mock, store, 2, zap.NewNop())

	err := sut.Publish(context.Background(), nil, newPath(t, "/ipns/example.com"))
	if !errors.Is(err, publishErr) {
		t.Fatalf("expected publishErr, got %v", err)
	}
	sut.Stop()
}

func TestIPNSStore_PutGet(t *testing.T) {
	store := newTestStore(t)

	p := newPath(t, "/ipns/example.com")
	entry := staleEntry{
		result:   namesys.Result{Path: p, TTL: time.Minute},
		cachedAt: time.Now(),
	}

	store.PutStale("/ipns/example.com", entry)
	got, ok := store.GetStale("/ipns/example.com")
	if !ok {
		t.Fatal("expected stale entry to be found")
	}
	if got.result.Path.String() != p.String() {
		t.Errorf("expected path %s, got %s", p.String(), got.result.Path.String())
	}
}

func TestIPNSStore_Delete(t *testing.T) {
	store := newTestStore(t)

	p := newPath(t, "/ipns/example.com")
	store.PutStale("/ipns/example.com", staleEntry{
		result:   namesys.Result{Path: p},
		cachedAt: time.Now(),
	})
	store.DeleteStale("/ipns/example.com")

	_, ok := store.GetStale("/ipns/example.com")
	if ok {
		t.Error("expected stale entry to be deleted")
	}
}

func TestIPNSStore_PersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()

	p := newPath(t, "/ipns/example.com")

	store1, err := NewIPNSStore(dir, 5*time.Minute, 30*time.Second, 128, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	store1.PutStale("/ipns/example.com", staleEntry{
		result:   namesys.Result{Path: p, TTL: time.Minute},
		cachedAt: time.Now(),
	})
	_ = store1.Close()

	store2, err := NewIPNSStore(dir, 5*time.Minute, 30*time.Second, 128, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store2.Close() }()

	got, ok := store2.GetStale("/ipns/example.com")
	if !ok {
		t.Fatal("expected stale entry to survive restart")
	}
	if got.result.Path.String() != p.String() {
		t.Errorf("expected path %s, got %s", p.String(), got.result.Path.String())
	}
}

func TestIPNSStore_ExpiredEntriesPrunedOnLoad(t *testing.T) {
	dir := t.TempDir()

	p := newPath(t, "/ipns/example.com")

	store1, err := NewIPNSStore(dir, 1*time.Nanosecond, 30*time.Second, 128, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	store1.PutStale("/ipns/example.com", staleEntry{
		result:   namesys.Result{Path: p},
		cachedAt: time.Now().Add(-time.Hour),
	})
	_ = store1.Close()

	store2, err := NewIPNSStore(dir, 1*time.Nanosecond, 30*time.Second, 128, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store2.Close() }()

	_, ok := store2.GetStale("/ipns/example.com")
	if ok {
		t.Error("expected expired stale entry to be pruned on load")
	}
}

func TestIPNSStore_LRUEviction(t *testing.T) {
	store, err := NewIPNSStore(t.TempDir(), 5*time.Minute, 30*time.Second, 3, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	for i := 0; i < 5; i++ {
		p := newPath(t, "/ipns/example.com")
		store.PutStale("/ipns/"+string(rune('a'+i)), staleEntry{
			result:   namesys.Result{Path: p},
			cachedAt: time.Now(),
		})
	}

	if store.stale.Len() > 3 {
		t.Errorf("expected at most 3 LRU entries, got %d", store.stale.Len())
	}
}

func TestDefaultMaxWorkers(t *testing.T) {
	mock := &mockNameSystem{}
	store := newTestStore(t)
	sut := NewStaleWhileRevalidateNameSystem(mock, store, 0, zap.NewNop())
	if sut.pool.Size() != 4 {
		t.Errorf("expected default maxWorkers=4, got %d", sut.pool.Size())
	}
	sut.Stop()
}
