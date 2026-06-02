package cache

import (
	"testing"
	"time"

	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
)

func TestNewStatusCache(t *testing.T) {
	size := 100
	ttl := 5 * time.Minute

	cache, err := NewStatusCacheSimple(size, ttl, 30*time.Second)
	if err != nil {
		t.Fatalf("NewStatusCache returned error: %v", err)
	}

	if cache.ttl != ttl {
		t.Errorf("expected ttl %v, got %v", ttl, cache.ttl)
	}

	if cache.cache == nil {
		t.Error("expected cache.cache to be initialized")
	}
}

func TestStatusCache_Get_Miss(t *testing.T) {
	cache, err := NewStatusCacheSimple(10, 5*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatalf("NewStatusCache returned error: %v", err)
	}

	result := cache.Get("example.com")

	if result.Hit {
		t.Error("expected cache miss, got hit")
	}

	if result.Entry != nil {
		t.Error("expected nil entry on miss")
	}

	if result.Expired {
		t.Error("expected expired to be false on miss")
	}
}

func TestStatusCache_SetAndGet_Hit(t *testing.T) {
	cache, err := NewStatusCacheSimple(10, 5*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatalf("NewStatusCache returned error: %v", err)
	}

	response := &types.GatewayWebsiteResponse{
		Domain:     "example.com",
		TargetType: "ipfs",
		TargetHash: "QmExample",
		Status:     types.StatusActive,
	}

	cache.Set("example.com", response)
	result := cache.Get("example.com")

	if !result.Hit {
		t.Error("expected cache hit, got miss")
	}

	if result.Expired {
		t.Error("expected entry not to be expired")
	}

	if result.Entry == nil {
		t.Fatal("expected non-nil entry")
	}

	if result.Entry.Response != response {
		t.Error("expected cached response to match")
	}

	if result.Entry.Response.Domain != "example.com" {
		t.Errorf("expected domain example.com, got %s", result.Entry.Response.Domain)
	}
}

func TestStatusCache_SetAndGet_Expired(t *testing.T) {
	cache, err := NewStatusCacheSimple(10, 10*time.Millisecond, 30*time.Second)
	if err != nil {
		t.Fatalf("NewStatusCache returned error: %v", err)
	}

	response := &types.GatewayWebsiteResponse{
		Domain:     "example.com",
		TargetType: "ipfs",
		TargetHash: "QmExample",
		Status:     types.StatusActive,
	}

	cache.Set("example.com", response)

	time.Sleep(20 * time.Millisecond)

	result := cache.Get("example.com")

	if !result.Hit {
		t.Error("expected cache hit (expired counts as hit)")
	}

	if !result.Expired {
		t.Error("expected entry to be expired")
	}

	if result.Entry == nil {
		t.Error("expected stale entry when expired (stale-while-revalidate)")
	}

	if result.Entry.Response == nil || result.Entry.Response.Domain != "example.com" {
		t.Error("expected stale entry to contain original response data")
	}

	result2 := cache.Get("example.com")
	if !result2.Hit {
		t.Error("expected cache hit for stale entry")
	}

	if !result2.Expired {
		t.Error("expected stale entry to still be expired")
	}
}

func TestStatusCache_SetInvalid(t *testing.T) {
	cache, err := NewStatusCacheSimple(10, 5*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatalf("NewStatusCache returned error: %v", err)
	}

	cache.SetInvalid("invalid.com")
	result := cache.Get("invalid.com")

	if !result.Hit {
		t.Error("expected cache hit for invalid domain")
	}

	if result.Expired {
		t.Error("expected entry not to be expired")
	}

	if result.Entry == nil {
		t.Fatal("expected non-nil entry")
	}

	if result.Entry.Response != nil {
		t.Error("expected nil response for invalid domain")
	}
}

func TestStatusCache_LRUEviction(t *testing.T) {
	cache, err := NewStatusCacheSimple(3, 5*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatalf("NewStatusCache returned error: %v", err)
	}

	cache.Set("domain1.com", &types.GatewayWebsiteResponse{Domain: "domain1.com"})
	cache.Set("domain2.com", &types.GatewayWebsiteResponse{Domain: "domain2.com"})
	cache.Set("domain3.com", &types.GatewayWebsiteResponse{Domain: "domain3.com"})

	result1 := cache.Get("domain1.com")
	if !result1.Hit {
		t.Error("expected domain1.com to be cached")
	}

	cache.Set("domain4.com", &types.GatewayWebsiteResponse{Domain: "domain4.com"})

	result1After := cache.Get("domain1.com")
	if !result1After.Hit {
		t.Error("expected domain1.com to still be cached (recently accessed)")
	}

	result2 := cache.Get("domain2.com")
	if result2.Hit {
		t.Error("expected domain2.com to be evicted (LRU - not accessed)")
	}

	result3 := cache.Get("domain3.com")
	if !result3.Hit {
		t.Error("expected domain3.com to still be cached")
	}

	result4 := cache.Get("domain4.com")
	if !result4.Hit {
		t.Error("expected domain4.com to be cached")
	}
}

func TestStatusCache_LRUEviction_AccessOrder(t *testing.T) {
	cache, err := NewStatusCacheSimple(3, 5*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatalf("NewStatusCache returned error: %v", err)
	}

	cache.Set("domain1.com", &types.GatewayWebsiteResponse{Domain: "domain1.com"})
	cache.Set("domain2.com", &types.GatewayWebsiteResponse{Domain: "domain2.com"})
	cache.Set("domain3.com", &types.GatewayWebsiteResponse{Domain: "domain3.com"})

	cache.Get("domain1.com")

	cache.Set("domain4.com", &types.GatewayWebsiteResponse{Domain: "domain4.com"})

	result1 := cache.Get("domain1.com")
	if !result1.Hit {
		t.Error("expected domain1.com to still be cached (recently accessed)")
	}

	result2 := cache.Get("domain2.com")
	if result2.Hit {
		t.Error("expected domain2.com to be evicted (LRU)")
	}

	result3 := cache.Get("domain3.com")
	if !result3.Hit {
		t.Error("expected domain3.com to still be cached")
	}

	result4 := cache.Get("domain4.com")
	if !result4.Hit {
		t.Error("expected domain4.com to be cached")
	}
}

func TestStatusCache_CacheMetadata(t *testing.T) {
	cache, err := NewStatusCacheSimple(10, 5*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatalf("NewStatusCache returned error: %v", err)
	}

	response := &types.GatewayWebsiteResponse{
		Domain:     "example.com",
		TargetType: "ipfs",
		TargetHash: "QmExample",
		Status:     types.StatusActive,
	}

	beforeSet := time.Now()
	cache.Set("example.com", response)
	afterSet := time.Now()

	result := cache.Get("example.com")

	if result.Entry == nil {
		t.Fatal("expected non-nil entry")
	}

	if result.Entry.CachedAt.Before(beforeSet) || result.Entry.CachedAt.After(afterSet) {
		t.Error("CachedAt time is not within expected range")
	}

	expectedExpiry := result.Entry.CachedAt.Add(cache.ttl)
	if result.Entry.ExpiresAt != expectedExpiry {
		t.Errorf("expected ExpiresAt %v, got %v", expectedExpiry, result.Entry.ExpiresAt)
	}
}

func TestStatusCache_OverwriteEntry(t *testing.T) {
	cache, err := NewStatusCacheSimple(10, 5*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatalf("NewStatusCache returned error: %v", err)
	}

	response1 := &types.GatewayWebsiteResponse{
		Domain:     "example.com",
		TargetType: "ipfs",
		TargetHash: "QmFirst",
		Status:     types.StatusActive,
	}

	cache.Set("example.com", response1)

	response2 := &types.GatewayWebsiteResponse{
		Domain:     "example.com",
		TargetType: "ipns",
		TargetHash: "k51qzi5uqu5dj13mw8qc7c4",
		Status:     types.StatusActive,
	}

	cache.Set("example.com", response2)

	result := cache.Get("example.com")

	if !result.Hit {
		t.Error("expected cache hit")
	}

	if result.Entry.Response.TargetHash != "k51qzi5uqu5dj13mw8qc7c4" {
		t.Errorf("expected updated target hash, got %s", result.Entry.Response.TargetHash)
	}

	if result.Entry.Response.TargetType != "ipns" {
		t.Errorf("expected updated target type, got %s", result.Entry.Response.TargetType)
	}
}

func TestStatusCache_EmptyDomain(t *testing.T) {
	cache, err := NewStatusCacheSimple(10, 5*time.Minute, 30*time.Second)
	if err != nil {
		t.Fatalf("NewStatusCache returned error: %v", err)
	}

	cache.Set("", &types.GatewayWebsiteResponse{Domain: ""})
	result := cache.Get("")

	if !result.Hit {
		t.Error("expected cache hit for empty domain")
	}
}

func TestNewStatusCache_InvalidSize(t *testing.T) {
	t.Run("zero size", func(t *testing.T) {
		_, err := NewStatusCacheSimple(0, 5*time.Minute, 30*time.Second)
		if err == nil {
			t.Error("expected error for zero size")
		}
		if err.Error() != "cache size must be positive" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("negative size", func(t *testing.T) {
		_, err := NewStatusCacheSimple(-10, 5*time.Minute, 30*time.Second)
		if err == nil {
			t.Error("expected error for negative size")
		}
		if err.Error() != "cache size must be positive" {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestNewStatusCache_InvalidTTL(t *testing.T) {
	t.Run("zero ttl", func(t *testing.T) {
		_, err := NewStatusCacheSimple(10, 0, 30*time.Second)
		if err == nil {
			t.Error("expected error for zero TTL")
		}
		if err.Error() != "cache TTL must be positive" {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("negative ttl", func(t *testing.T) {
		_, err := NewStatusCacheSimple(10, -5*time.Minute, 30*time.Second)
		if err == nil {
			t.Error("expected error for negative TTL")
		}
		if err.Error() != "cache TTL must be positive" {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}
