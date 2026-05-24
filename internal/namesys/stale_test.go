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
	store, err := NewIPNSStore(t.TempDir(), 30*time.Second, 30*time.Second, 128, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestResolvableKey_NormalizesSubpaths(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/ipns/12D3KooWabc", "/ipns/12D3KooWabc"},
		{"/ipns/12D3KooWabc/assets/style.css", "/ipns/12D3KooWabc"},
		{"/ipns/12D3KooWabc/deep/nested/path.js", "/ipns/12D3KooWabc"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			p := newPath(t, tt.input)
			got := resolvableKey(p)
			if got != tt.expected {
				t.Errorf("resolvableKey(%s) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestJoinSubPath_AppendsRemaining(t *testing.T) {
	base := newPath(t, "/ipns/12D3KooWabc")

	joined, err := joinSubPath(base, newPath(t, "/ipns/12D3KooWabc/assets/style.css"))
	if err != nil {
		t.Fatal(err)
	}
	if joined.String() != "/ipns/12D3KooWabc/assets/style.css" {
		t.Errorf("got %s", joined.String())
	}

	joined2, err := joinSubPath(base, newPath(t, "/ipns/12D3KooWabc/deep/nested.js"))
	if err != nil {
		t.Fatal(err)
	}
	if joined2.String() != "/ipns/12D3KooWabc/deep/nested.js" {
		t.Errorf("got %s", joined2.String())
	}

	joined3, err := joinSubPath(base, newPath(t, "/ipns/12D3KooWabc"))
	if err != nil {
		t.Fatal(err)
	}
	if joined3.String() != "/ipns/12D3KooWabc" {
		t.Errorf("base-only should return base, got %s", joined3.String())
	}
}

func TestStripSubPath_TruncatesToBase(t *testing.T) {
	result := namesys.Result{Path: newPath(t, "/ipns/12D3KooWabc/assets/style.css"), TTL: time.Minute}

	stripped := stripSubPath(result, "/ipns/12D3KooWabc")
	if stripped.Path.String() != "/ipns/12D3KooWabc" {
		t.Errorf("expected base path, got %s", stripped.Path.String())
	}

	result2 := namesys.Result{Path: newPath(t, "/ipns/12D3KooWabc"), TTL: time.Minute}
	stripped2 := stripSubPath(result2, "/ipns/12D3KooWabc")
	if stripped2.Path.String() != "/ipns/12D3KooWabc" {
		t.Errorf("already-base should stay base, got %s", stripped2.Path.String())
	}
}

func TestResolve_SubpathsShareCacheEntry(t *testing.T) {
	var resolveCall atomic.Int32
	resolvedBase := newPath(t, "/ipns/12D3KooWabc")

	mock := &mockNameSystem{
		resolveFn: func(ctx context.Context, req path.Path, opts ...namesys.ResolveOption) (namesys.Result, error) {
			resolveCall.Add(1)
			segments := req.Segments()
			if len(segments) > 2 {
				joined, err := path.Join(resolvedBase, segments[2:]...)
				if err != nil {
					return namesys.Result{}, err
				}
				return namesys.Result{Path: joined, TTL: time.Minute}, nil
			}
			return namesys.Result{Path: resolvedBase, TTL: time.Minute}, nil
		},
	}

	store := newTestStore(t)
	sut := NewStaleWhileRevalidateNameSystem(mock, store, 2, zap.NewNop())

	subPath := newPath(t, "/ipns/12D3KooWabc/assets/style.css")
	result, err := sut.Resolve(context.Background(), subPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	expected := "/ipns/12D3KooWabc/assets/style.css"
	if result.Path.String() != expected {
		t.Errorf("first resolve: expected %s, got %s", expected, result.Path.String())
	}
	if resolveCall.Load() != 1 {
		t.Fatalf("expected 1 inner resolve call, got %d", resolveCall.Load())
	}

	anotherSubPath := newPath(t, "/ipns/12D3KooWabc/deep/nested.js")
	result2, err := sut.Resolve(context.Background(), anotherSubPath)
	if err != nil {
		t.Fatalf("expected no error on second sub-path, got %v", err)
	}
	expected2 := "/ipns/12D3KooWabc/deep/nested.js"
	if result2.Path.String() != expected2 {
		t.Errorf("cache hit: expected %s, got %s", expected2, result2.Path.String())
	}
	if resolveCall.Load() != 1 {
		t.Errorf("sub-path should hit cache, expected 1 total resolve call, got %d", resolveCall.Load())
	}

	baseOnly := newPath(t, "/ipns/12D3KooWabc")
	result3, err := sut.Resolve(context.Background(), baseOnly)
	if err != nil {
		t.Fatalf("expected no error on base path, got %v", err)
	}
	if result3.Path.String() != "/ipns/12D3KooWabc" {
		t.Errorf("base path: expected /ipns/12D3KooWabc, got %s", result3.Path.String())
	}
	if resolveCall.Load() != 1 {
		t.Errorf("base path should also hit cache, expected 1 total resolve call, got %d", resolveCall.Load())
	}

	sut.Stop()
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

func TestResolve_FreshWindow_NoRevalidation(t *testing.T) {
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

	result, err := sut.Resolve(context.Background(), p)
	if err != nil {
		t.Fatalf("expected no error from fresh cache, got %v", err)
	}
	if result.Path.String() != p.String() {
		t.Errorf("expected path %s, got %s", p.String(), result.Path.String())
	}

	if mock.resolveCall.Load() != 0 {
		t.Errorf("fresh window should not trigger revalidation, got %d calls", mock.resolveCall.Load())
	}
	sut.Stop()
}

func TestResolve_StaleWindow_TriggersRevalidation(t *testing.T) {
	p := newPath(t, "/ipns/example.com")
	newP := newPath(t, "/ipns/new.example.com")

	var resolveCall atomic.Int32
	mock := &mockNameSystem{
		resolveFn: func(ctx context.Context, req path.Path, opts ...namesys.ResolveOption) (namesys.Result, error) {
			n := resolveCall.Add(1)
			if n == 1 {
				return namesys.Result{Path: newP, TTL: time.Minute}, nil
			}
			return namesys.Result{}, errTestResolve
		},
	}

	store, err := NewIPNSStore(t.TempDir(), 30*time.Second, 30*time.Second, 128, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	sut := NewStaleWhileRevalidateNameSystem(mock, store, 2, zap.NewNop())

	store.PutStale(p.String(), staleEntry{
		result:   namesys.Result{Path: p},
		cachedAt: time.Now().Add(-time.Minute),
	})

	result, err := sut.Resolve(context.Background(), p)
	if err != nil {
		t.Fatalf("expected no error from stale cache, got %v", err)
	}
	if result.Path.String() != p.String() {
		t.Errorf("expected stale path %s, got %s", p.String(), result.Path.String())
	}

	sut.Stop()

	if resolveCall.Load() != 1 {
		t.Errorf("stale window should trigger 1 revalidation, got %d calls", resolveCall.Load())
	}

	updated, ok := store.GetStale(p.String())
	if !ok {
		t.Fatal("expected stale entry to be updated after revalidation")
	}
	if updated.result.Path.String() != newP.String() {
		t.Errorf("expected stale entry updated to %s, got %s", newP.String(), updated.result.Path.String())
	}
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

func TestResolveAsync_StaleServedWithoutError(t *testing.T) {
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
		cachedAt: time.Now().Add(-time.Hour),
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

	store1, err := NewIPNSStore(dir, 30*time.Second, 30*time.Second, 128, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	store1.PutStale("/ipns/example.com", staleEntry{
		result:   namesys.Result{Path: p, TTL: time.Minute},
		cachedAt: time.Now(),
	})
	_ = store1.Close()

	store2, err := NewIPNSStore(dir, 30*time.Second, 30*time.Second, 128, zap.NewNop())
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

func TestIPNSStore_OldEntriesSurviveRestart(t *testing.T) {
	dir := t.TempDir()

	p := newPath(t, "/ipns/example.com")

	store1, err := NewIPNSStore(dir, 30*time.Second, 30*time.Second, 128, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	store1.PutStale("/ipns/example.com", staleEntry{
		result:   namesys.Result{Path: p},
		cachedAt: time.Now().Add(-time.Hour),
	})
	_ = store1.Close()

	store2, err := NewIPNSStore(dir, 30*time.Second, 30*time.Second, 128, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store2.Close() }()

	got, ok := store2.GetStale("/ipns/example.com")
	if !ok {
		t.Fatal("expected old stale entry to survive restart (no age-based eviction)")
	}
	if got.result.Path.String() != p.String() {
		t.Errorf("expected path %s, got %s", p.String(), got.result.Path.String())
	}
}

func TestIPNSStore_LRUEviction(t *testing.T) {
	store, err := NewIPNSStore(t.TempDir(), 30*time.Second, 30*time.Second, 3, zap.NewNop())
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

func TestResolveAsync_PropagatesError(t *testing.T) {
	p := newPath(t, "/ipns/example.com")

	mock := &mockNameSystem{
		resolveFn: func(ctx context.Context, req path.Path, opts ...namesys.ResolveOption) (namesys.Result, error) {
			return namesys.Result{}, errTestResolve
		},
	}

	store := newTestStore(t)
	sut := NewStaleWhileRevalidateNameSystem(mock, store, 2, zap.NewNop())

	ch := sut.ResolveAsync(context.Background(), p)
	res := <-ch
	if res.Err == nil {
		t.Fatal("expected error from async resolve with no stale fallback, got nil")
	}
	if !errors.Is(res.Err, errTestResolve) {
		t.Errorf("expected errTestResolve, got %v", res.Err)
	}
	sut.Stop()
}

func TestResolve_RespectsContextTimeout(t *testing.T) {
	p := newPath(t, "/ipns/example.com")

	mock := &mockNameSystem{
		resolveFn: func(ctx context.Context, req path.Path, opts ...namesys.ResolveOption) (namesys.Result, error) {
			<-ctx.Done()
			return namesys.Result{}, ctx.Err()
		},
	}

	store := newTestStore(t)
	sut := NewStaleWhileRevalidateNameSystem(mock, store, 2, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ch := sut.ResolveAsync(ctx, p)
	select {
	case res := <-ch:
		if res.Err == nil {
			t.Error("expected error from timed out resolve, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ResolveAsync hung — context timeout not working")
	}
	sut.Stop()
}

func TestResolve_FastPath_ReturnsImmediatelyWithStaleCache(t *testing.T) {
	p := newPath(t, "/ipns/example.com")

	blockCh := make(chan struct{})
	mock := &mockNameSystem{
		resolveFn: func(ctx context.Context, req path.Path, opts ...namesys.ResolveOption) (namesys.Result, error) {
			<-blockCh
			return namesys.Result{Path: p}, nil
		},
	}

	store, err := NewIPNSStore(t.TempDir(), 30*time.Second, 30*time.Second, 128, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	sut := NewStaleWhileRevalidateNameSystem(mock, store, 2, zap.NewNop())

	store.PutStale(p.String(), staleEntry{
		result:   namesys.Result{Path: p, TTL: time.Minute},
		cachedAt: time.Now(),
	})

	start := time.Now()
	result, err := sut.Resolve(context.Background(), p)
	elapsed := time.Since(start)

	close(blockCh)
	sut.Stop()

	if err != nil {
		t.Fatalf("expected no error from fast path, got %v", err)
	}
	if result.Path.String() != p.String() {
		t.Errorf("expected path %s, got %s", p.String(), result.Path.String())
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("fast path should return immediately, took %v", elapsed)
	}
}

func TestResolveAsync_FastPath_ReturnsImmediatelyWithStaleCache(t *testing.T) {
	p := newPath(t, "/ipns/example.com")

	blockCh := make(chan struct{})
	mock := &mockNameSystem{
		resolveFn: func(ctx context.Context, req path.Path, opts ...namesys.ResolveOption) (namesys.Result, error) {
			<-blockCh
			return namesys.Result{Path: p}, nil
		},
	}

	store, err := NewIPNSStore(t.TempDir(), 30*time.Second, 30*time.Second, 128, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	sut := NewStaleWhileRevalidateNameSystem(mock, store, 2, zap.NewNop())

	store.PutStale(p.String(), staleEntry{
		result:   namesys.Result{Path: p, TTL: time.Minute},
		cachedAt: time.Now(),
	})

	start := time.Now()
	ch := sut.ResolveAsync(context.Background(), p)
	res := <-ch
	elapsed := time.Since(start)

	close(blockCh)
	sut.Stop()

	if res.Err != nil {
		t.Fatalf("expected no error from async fast path, got %v", res.Err)
	}
	if res.Path.String() != p.String() {
		t.Errorf("expected path %s, got %s", p.String(), res.Path.String())
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("async fast path should return immediately, took %v", elapsed)
	}
}

func TestResolve_TimesOut_NoStale_ReturnsError(t *testing.T) {
	p := newPath(t, "/ipns/example.com")

	mock := &mockNameSystem{
		resolveFn: func(ctx context.Context, req path.Path, opts ...namesys.ResolveOption) (namesys.Result, error) {
			<-ctx.Done()
			return namesys.Result{}, ctx.Err()
		},
	}

	store, err := NewIPNSStore(t.TempDir(), 5*time.Minute, 100*time.Millisecond, 128, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	sut := NewStaleWhileRevalidateNameSystem(mock, store, 2, zap.NewNop())

	done := make(chan error, 1)
	go func() {
		_, err := sut.Resolve(context.Background(), p)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from timed out resolve with no stale fallback, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Resolve hung — timeout not working")
	}
	sut.Stop()
}
