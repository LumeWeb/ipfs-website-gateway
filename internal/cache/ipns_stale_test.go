package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"go.uber.org/zap"
)

func newTestIPNSStaleStore(t *testing.T) (*IPNSStaleStore, redismock.ClientMock) {
	t.Helper()
	client, mock := redismock.NewClientMock()
	rc := newRedisClientWithClient(client, "gateway:")
	store := NewIPNSStaleStore(rc, 30*time.Second, zap.NewNop())
	return store, mock
}

func marshalStaleEntry(t *testing.T, entry StaleEntry) string {
	t.Helper()
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("failed to marshal stale entry: %v", err)
	}
	return string(data)
}

func TestIPNSStaleStore_PutStale(t *testing.T) {
	store, mock := newTestIPNSStaleStore(t)
	ctx := context.Background()

	entry := StaleEntry{
		Path:     "/ipfs/bafybeihqjmf3b7z",
		TTL:      int64(60 * time.Second),
		LastMod:  time.Now().Unix(),
		CachedAt: time.Now().Unix(),
	}
	data, _ := json.Marshal(entry)

	mock.ExpectSet("gateway:ipns:stale:/ipns/12D3KooWabc", data, 30*time.Second).SetVal("OK")

	if err := store.PutStale(ctx, "/ipns/12D3KooWabc", entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestIPNSStaleStore_GetStale_Hit(t *testing.T) {
	store, mock := newTestIPNSStaleStore(t)
	ctx := context.Background()

	entry := StaleEntry{
		Path:     "/ipfs/bafybeihqjmf3b7z",
		TTL:      int64(60 * time.Second),
		LastMod:  time.Now().Unix(),
		CachedAt: time.Now().Unix(),
	}

	mock.ExpectGet("gateway:ipns:stale:/ipns/12D3KooWabc").SetVal(marshalStaleEntry(t, entry))

	result, err := store.GetStale(ctx, "/ipns/12D3KooWabc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Path != entry.Path {
		t.Errorf("expected path %s, got %s", entry.Path, result.Path)
	}
	if result.TTL != entry.TTL {
		t.Errorf("expected TTL %d, got %d", entry.TTL, result.TTL)
	}
	if result.LastMod != entry.LastMod {
		t.Errorf("expected LastMod %d, got %d", entry.LastMod, result.LastMod)
	}
	if result.CachedAt != entry.CachedAt {
		t.Errorf("expected CachedAt %d, got %d", entry.CachedAt, result.CachedAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestIPNSStaleStore_GetStale_Miss(t *testing.T) {
	store, mock := newTestIPNSStaleStore(t)
	ctx := context.Background()

	mock.ExpectGet("gateway:ipns:stale:/ipns/missing").RedisNil()

	_, err := store.GetStale(ctx, "/ipns/missing")
	if err == nil {
		t.Fatal("expected error on miss")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestIPNSStaleStore_GetStale_RedisError(t *testing.T) {
	store, mock := newTestIPNSStaleStore(t)
	ctx := context.Background()

	mock.ExpectGet("gateway:ipns:stale:/ipns/fail").SetErr(fmt.Errorf("connection refused"))

	_, err := store.GetStale(ctx, "/ipns/fail")
	if err == nil {
		t.Fatal("expected error from redis failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestIPNSStaleStore_GetStale_CorruptData(t *testing.T) {
	store, mock := newTestIPNSStaleStore(t)
	ctx := context.Background()

	mock.ExpectGet("gateway:ipns:stale:/ipns/bad").SetVal("not-json")

	_, err := store.GetStale(ctx, "/ipns/bad")
	if err == nil {
		t.Fatal("expected error from corrupt data")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestIPNSStaleStore_DeleteStale(t *testing.T) {
	store, mock := newTestIPNSStaleStore(t)
	ctx := context.Background()

	mock.ExpectDel("gateway:ipns:stale:/ipns/12D3KooWabc").SetVal(1)

	if err := store.DeleteStale(ctx, "/ipns/12D3KooWabc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestIPNSStaleStore_DeleteStale_RedisError(t *testing.T) {
	store, mock := newTestIPNSStaleStore(t)
	ctx := context.Background()

	mock.ExpectDel("gateway:ipns:stale:/ipns/fail").SetErr(fmt.Errorf("connection refused"))

	if err := store.DeleteStale(ctx, "/ipns/fail"); err == nil {
		t.Fatal("expected error from redis failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestIPNSStaleStore_LoadAllStale(t *testing.T) {
	store, mock := newTestIPNSStaleStore(t)
	ctx := context.Background()

	entry := StaleEntry{
		Path:     "/ipfs/bafybeihqjmf3b7z",
		TTL:      int64(60 * time.Second),
		LastMod:  time.Now().Unix(),
		CachedAt: time.Now().Unix(),
	}

	mock.ExpectScan(0, "gateway:ipns:stale:*", 100).SetVal([]string{"gateway:ipns:stale:/ipns/12D3KooWabc"}, 0)
	mock.ExpectGet("gateway:ipns:stale:/ipns/12D3KooWabc").SetVal(marshalStaleEntry(t, entry))

	entries, err := store.LoadAllStale(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	got, ok := entries["/ipns/12D3KooWabc"]
	if !ok {
		t.Fatal("expected entry for /ipns/12D3KooWabc")
	}
	if got.Path != entry.Path {
		t.Errorf("expected path %s, got %s", entry.Path, got.Path)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestIPNSStaleStore_LoadAllStale_SkipsCorrupt(t *testing.T) {
	store, mock := newTestIPNSStaleStore(t)
	ctx := context.Background()

	mock.ExpectScan(0, "gateway:ipns:stale:*", 100).SetVal([]string{"gateway:ipns:stale:/ipns/bad"}, 0)
	mock.ExpectGet("gateway:ipns:stale:/ipns/bad").SetVal("not-json")

	entries, err := store.LoadAllStale(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries (corrupt), got %d", len(entries))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestIPNSStaleStore_LoadAllStale_SkipsNilValues(t *testing.T) {
	store, mock := newTestIPNSStaleStore(t)
	ctx := context.Background()

	mock.ExpectScan(0, "gateway:ipns:stale:*", 100).SetVal([]string{"gateway:ipns:stale:/ipns/gone"}, 0)
	mock.ExpectGet("gateway:ipns:stale:/ipns/gone").RedisNil()

	entries, err := store.LoadAllStale(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries (nil value), got %d", len(entries))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestIPNSStaleStore_CustomPrefix_Put(t *testing.T) {
	client, mock := redismock.NewClientMock()
	rc := newRedisClientWithClient(client, "myapp:")
	store := NewIPNSStaleStore(rc, 30*time.Second, zap.NewNop())
	ctx := context.Background()

	entry := StaleEntry{
		Path:     "/ipfs/bafybeihqjmf3b7z",
		TTL:      int64(60 * time.Second),
		LastMod:  time.Now().Unix(),
		CachedAt: time.Now().Unix(),
	}
	data, _ := json.Marshal(entry)

	mock.ExpectSet("myapp:ipns:stale:/ipns/12D3KooWabc", data, 30*time.Second).SetVal("OK")

	if err := store.PutStale(ctx, "/ipns/12D3KooWabc", entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestIPNSStaleStore_CustomPrefix_Get(t *testing.T) {
	client, mock := redismock.NewClientMock()
	rc := newRedisClientWithClient(client, "myapp:")
	store := NewIPNSStaleStore(rc, 30*time.Second, zap.NewNop())
	ctx := context.Background()

	entry := StaleEntry{
		Path:     "/ipfs/bafybeihqjmf3b7z",
		TTL:      int64(60 * time.Second),
		LastMod:  time.Now().Unix(),
		CachedAt: time.Now().Unix(),
	}

	mock.ExpectGet("myapp:ipns:stale:/ipns/12D3KooWabc").SetVal(marshalStaleEntry(t, entry))

	result, err := store.GetStale(ctx, "/ipns/12D3KooWabc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Path != entry.Path {
		t.Errorf("expected path %s, got %s", entry.Path, result.Path)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestIPNSStaleStore_CustomPrefix_Delete(t *testing.T) {
	client, mock := redismock.NewClientMock()
	rc := newRedisClientWithClient(client, "myapp:")
	store := NewIPNSStaleStore(rc, 30*time.Second, zap.NewNop())
	ctx := context.Background()

	mock.ExpectDel("myapp:ipns:stale:/ipns/12D3KooWabc").SetVal(1)

	if err := store.DeleteStale(ctx, "/ipns/12D3KooWabc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestIPNSStaleStore_CustomPrefix_LoadAll(t *testing.T) {
	client, mock := redismock.NewClientMock()
	rc := newRedisClientWithClient(client, "myapp:")
	store := NewIPNSStaleStore(rc, 30*time.Second, zap.NewNop())
	ctx := context.Background()

	entry := StaleEntry{
		Path:     "/ipfs/bafybeihqjmf3b7z",
		TTL:      int64(60 * time.Second),
		LastMod:  time.Now().Unix(),
		CachedAt: time.Now().Unix(),
	}

	mock.ExpectScan(0, "myapp:ipns:stale:*", 100).SetVal([]string{"myapp:ipns:stale:/ipns/key1"}, 0)
	mock.ExpectGet("myapp:ipns:stale:/ipns/key1").SetVal(marshalStaleEntry(t, entry))

	entries, err := store.LoadAllStale(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if _, ok := entries["/ipns/key1"]; !ok {
		t.Error("expected entry for /ipns/key1")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestIPNSStaleStore_Close(t *testing.T) {
	store, _ := newTestIPNSStaleStore(t)
	if err := store.Close(); err != nil {
		t.Errorf("unexpected Close error: %v", err)
	}
}
