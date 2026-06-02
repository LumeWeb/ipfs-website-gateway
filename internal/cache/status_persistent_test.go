package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
	"go.uber.org/zap"
)

func newTestStatusPersistentCache(t *testing.T) (*StatusPersistentCache, redismock.ClientMock) {
	t.Helper()
	client, mock := redismock.NewClientMock()
	rc := newRedisClientWithClient(client, "gateway:")
	spc := NewStatusPersistentCache(rc, zap.NewNop())
	return spc, mock
}

func marshalEntry(t *testing.T, entry *persistentCacheEntry) string {
	t.Helper()
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("failed to marshal entry: %v", err)
	}
	return string(data)
}

func TestStatusPersistentCache_Get_Hit(t *testing.T) {
	spc, mock := newTestStatusPersistentCache(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	pe := &persistentCacheEntry{
		Response:  &types.GatewayWebsiteResponse{Domain: "example.com", Status: types.StatusActive},
		CachedAt:  now,
		ExpiresAt: now.Add(5 * time.Minute),
	}

	mock.ExpectGet("gateway:status:example.com").SetVal(marshalEntry(t, pe))

	result, err := spc.Get(ctx, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Response == nil || result.Response.Domain != "example.com" {
		t.Errorf("expected domain example.com, got %v", result.Response)
	}
	if !result.CachedAt.Equal(pe.CachedAt) {
		t.Fatalf("expected CachedAt %v, got %v", pe.CachedAt, result.CachedAt)
	}
	if !result.ExpiresAt.Equal(pe.ExpiresAt) {
		t.Fatalf("expected ExpiresAt %v, got %v", pe.ExpiresAt, result.ExpiresAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStatusPersistentCache_Get_Miss(t *testing.T) {
	spc, mock := newTestStatusPersistentCache(t)
	ctx := context.Background()

	mock.ExpectGet("gateway:status:missing.com").RedisNil()

	result, err := spc.Get(ctx, "missing.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result on miss, got %v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStatusPersistentCache_Get_ErrorEntry(t *testing.T) {
	spc, mock := newTestStatusPersistentCache(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	pe := &persistentCacheEntry{
		ErrString: "some error",
		CachedAt:  now,
		ExpiresAt: now.Add(5 * time.Minute),
	}

	mock.ExpectGet("gateway:status:broken.com").SetVal(marshalEntry(t, pe))

	result, err := spc.Get(ctx, "broken.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for error entry")
	}
	if result.Err == nil {
		t.Error("expected error to be reconstructed from ErrString")
	}
	if result.Err.Error() != "some error" {
		t.Errorf("expected error 'some error', got %q", result.Err.Error())
	}
	if result.Response != nil {
		t.Error("expected nil Response for error-only entry")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStatusPersistentCache_Get_RedisError(t *testing.T) {
	spc, mock := newTestStatusPersistentCache(t)
	ctx := context.Background()

	mock.ExpectGet("gateway:status:fail.com").SetErr(fmt.Errorf("connection refused"))

	result, err := spc.Get(ctx, "fail.com")
	if err == nil {
		t.Fatal("expected error from redis failure")
	}
	if result != nil {
		t.Errorf("expected nil result on redis error, got %v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStatusPersistentCache_Get_CorruptData(t *testing.T) {
	spc, mock := newTestStatusPersistentCache(t)
	ctx := context.Background()

	mock.ExpectGet("gateway:status:bad.com").SetVal("not-json")

	result, err := spc.Get(ctx, "bad.com")
	if err == nil {
		t.Fatal("expected error from corrupt data")
	}
	if result != nil {
		t.Errorf("expected nil result on corrupt data, got %v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStatusPersistentCache_Set(t *testing.T) {
	spc, mock := newTestStatusPersistentCache(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	entry := &types.CacheEntry{
		Response:  &types.GatewayWebsiteResponse{Domain: "example.com", Status: types.StatusActive},
		CachedAt:  now,
		ExpiresAt: now.Add(5 * time.Minute),
	}

	pe := &persistentCacheEntry{
		Response:  entry.Response,
		CachedAt:  entry.CachedAt,
		ExpiresAt: entry.ExpiresAt,
	}
	data, _ := json.Marshal(pe)
	ttl := time.Until(entry.ExpiresAt)

	mock.ExpectSet("gateway:status:example.com", data, ttl).SetVal("OK")

	if err := spc.Set(ctx, "example.com", entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStatusPersistentCache_Set_ExpiredEntry_Skips(t *testing.T) {
	spc, mock := newTestStatusPersistentCache(t)
	ctx := context.Background()

	now := time.Now()
	entry := &types.CacheEntry{
		Response:  &types.GatewayWebsiteResponse{Domain: "old.com"},
		CachedAt:  now.Add(-10 * time.Minute),
		ExpiresAt: now.Add(-5 * time.Minute),
	}

	if err := spc.Set(ctx, "old.com", entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expected no redis calls for expired entry: %v", err)
	}
}

func TestStatusPersistentCache_Set_ErrorEntry(t *testing.T) {
	spc, mock := newTestStatusPersistentCache(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	entry := &types.CacheEntry{
		Err:       context.DeadlineExceeded,
		CachedAt:  now,
		ExpiresAt: now.Add(5 * time.Minute),
	}

	pe := &persistentCacheEntry{
		ErrString: context.DeadlineExceeded.Error(),
		CachedAt:  entry.CachedAt,
		ExpiresAt: entry.ExpiresAt,
	}
	data, _ := json.Marshal(pe)
	ttl := time.Until(entry.ExpiresAt)

	mock.ExpectSet("gateway:status:slow.com", data, ttl).SetVal("OK")

	if err := spc.Set(ctx, "slow.com", entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStatusPersistentCache_Delete(t *testing.T) {
	spc, mock := newTestStatusPersistentCache(t)
	ctx := context.Background()

	mock.ExpectDel("gateway:status:remove.com").SetVal(1)

	if err := spc.Delete(ctx, "remove.com"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStatusPersistentCache_Delete_RedisError(t *testing.T) {
	spc, mock := newTestStatusPersistentCache(t)
	ctx := context.Background()

	mock.ExpectDel("gateway:status:fail.com").SetErr(fmt.Errorf("connection refused"))

	if err := spc.Delete(ctx, "fail.com"); err == nil {
		t.Fatal("expected error from redis failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStatusPersistentCache_LoadAll(t *testing.T) {
	spc, mock := newTestStatusPersistentCache(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	pe1 := &persistentCacheEntry{
		Response:  &types.GatewayWebsiteResponse{Domain: "a.com", Status: types.StatusActive},
		CachedAt:  now,
		ExpiresAt: now.Add(5 * time.Minute),
	}
	pe2 := &persistentCacheEntry{
		Response:  &types.GatewayWebsiteResponse{Domain: "b.com", Status: types.StatusActive},
		CachedAt:  now,
		ExpiresAt: now.Add(5 * time.Minute),
	}

	mock.ExpectScan(0, "gateway:status:*", 100).SetVal([]string{"gateway:status:a.com", "gateway:status:b.com"}, 0)
	mock.ExpectGet("gateway:status:a.com").SetVal(marshalEntry(t, pe1))
	mock.ExpectGet("gateway:status:b.com").SetVal(marshalEntry(t, pe2))

	entries, err := spc.LoadAll(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if _, ok := entries["a.com"]; !ok {
		t.Error("expected entry for a.com")
	}
	if _, ok := entries["b.com"]; !ok {
		t.Error("expected entry for b.com")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStatusPersistentCache_LoadAll_SkipsExpired(t *testing.T) {
	spc, mock := newTestStatusPersistentCache(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	expired := &persistentCacheEntry{
		Response:  &types.GatewayWebsiteResponse{Domain: "old.com"},
		CachedAt:  now.Add(-10 * time.Minute),
		ExpiresAt: now.Add(-5 * time.Minute),
	}

	mock.ExpectScan(0, "gateway:status:*", 100).SetVal([]string{"gateway:status:old.com"}, 0)
	mock.ExpectGet("gateway:status:old.com").SetVal(marshalEntry(t, expired))

	entries, err := spc.LoadAll(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries (expired), got %d", len(entries))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStatusPersistentCache_LoadAll_SkipsCorrupt(t *testing.T) {
	spc, mock := newTestStatusPersistentCache(t)
	ctx := context.Background()

	mock.ExpectScan(0, "gateway:status:*", 100).SetVal([]string{"gateway:status:bad.com"}, 0)
	mock.ExpectGet("gateway:status:bad.com").SetVal("not-json")

	entries, err := spc.LoadAll(ctx)
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

func TestStatusPersistentCache_LoadAll_SkipsNilValues(t *testing.T) {
	spc, mock := newTestStatusPersistentCache(t)
	ctx := context.Background()

	mock.ExpectScan(0, "gateway:status:*", 100).SetVal([]string{"gateway:status:gone.com"}, 0)
	mock.ExpectGet("gateway:status:gone.com").RedisNil()

	entries, err := spc.LoadAll(ctx)
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

func TestStatusPersistentCache_CustomPrefix_Get(t *testing.T) {
	client, mock := redismock.NewClientMock()
	rc := newRedisClientWithClient(client, "myapp:")
	spc := NewStatusPersistentCache(rc, zap.NewNop())
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	pe := &persistentCacheEntry{
		Response:  &types.GatewayWebsiteResponse{Domain: "example.com", Status: types.StatusActive},
		CachedAt:  now,
		ExpiresAt: now.Add(5 * time.Minute),
	}

	mock.ExpectGet("myapp:status:example.com").SetVal(marshalEntry(t, pe))

	result, err := spc.Get(ctx, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Response == nil || result.Response.Domain != "example.com" {
		t.Errorf("expected hit for example.com with custom prefix")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStatusPersistentCache_CustomPrefix_Set(t *testing.T) {
	client, mock := redismock.NewClientMock()
	rc := newRedisClientWithClient(client, "myapp:")
	spc := NewStatusPersistentCache(rc, zap.NewNop())
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	entry := &types.CacheEntry{
		Response:  &types.GatewayWebsiteResponse{Domain: "example.com", Status: types.StatusActive},
		CachedAt:  now,
		ExpiresAt: now.Add(5 * time.Minute),
	}

	pe := &persistentCacheEntry{
		Response:  entry.Response,
		CachedAt:  entry.CachedAt,
		ExpiresAt: entry.ExpiresAt,
	}
	data, _ := json.Marshal(pe)
	ttl := time.Until(entry.ExpiresAt)

	mock.ExpectSet("myapp:status:example.com", data, ttl).SetVal("OK")

	if err := spc.Set(ctx, "example.com", entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStatusPersistentCache_CustomPrefix_LoadAll(t *testing.T) {
	client, mock := redismock.NewClientMock()
	rc := newRedisClientWithClient(client, "myapp:")
	spc := NewStatusPersistentCache(rc, zap.NewNop())
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	pe := &persistentCacheEntry{
		Response:  &types.GatewayWebsiteResponse{Domain: "x.com", Status: types.StatusActive},
		CachedAt:  now,
		ExpiresAt: now.Add(5 * time.Minute),
	}

	mock.ExpectScan(0, "myapp:status:*", 100).SetVal([]string{"myapp:status:x.com"}, 0)
	mock.ExpectGet("myapp:status:x.com").SetVal(marshalEntry(t, pe))

	entries, err := spc.LoadAll(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if _, ok := entries["x.com"]; !ok {
		t.Error("expected entry for x.com")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRedisClient_Key(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		suffix string
		want   string
	}{
		{"default prefix", "gateway:", "status:example.com", "gateway:status:example.com"},
		{"custom prefix", "myapp:", "status:example.com", "myapp:status:example.com"},
		{"ipns stale key", "gateway:", "ipns:stale:/ipns/12D3KooWabc", "gateway:ipns:stale:/ipns/12D3KooWabc"},
		{"empty prefix", "", "status:example.com", "status:example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &RedisClient{prefix: tt.prefix}
			got := r.Key(tt.suffix)
			if got != tt.want {
				t.Errorf("Key(%q) = %q, want %q", tt.suffix, got, tt.want)
			}
		})
	}
}

func TestRedisClient_Client_ReturnsUnderlying(t *testing.T) {
	client, _ := redismock.NewClientMock()
	rc := newRedisClientWithClient(client, "gateway:")
	if rc.Client() != client {
		t.Error("Client() should return the underlying *redis.Client")
	}
}

func TestRedisClient_Close(t *testing.T) {
	client, _ := redismock.NewClientMock()
	rc := newRedisClientWithClient(client, "gateway:")
	if err := rc.Close(); err != nil {
		t.Errorf("unexpected Close error: %v", err)
	}
}
