package cache

import (
	"errors"
	"time"

	"github.com/hashicorp/golang-lru/v2"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
)

// StatusCache provides an in-memory LRU cache with TTL for website status queries.
// It caches both positive and negative results (nil responses) to provide DoS protection
// and performance optimization.
type StatusCache struct {
	cache    *lru.Cache[string, *types.CacheEntry]
	ttl      time.Duration
	shortTTL time.Duration
}

func NewStatusCache(size int, ttl time.Duration, shortTTL time.Duration) (*StatusCache, error) {
	if size <= 0 {
		return nil, errors.New("cache size must be positive")
	}

	if ttl <= 0 {
		return nil, errors.New("cache TTL must be positive")
	}

	if shortTTL <= 0 {
		return nil, errors.New("cache short TTL must be positive")
	}

	cache, err := lru.New[string, *types.CacheEntry](size)
	if err != nil {
		return nil, err
	}

	return &StatusCache{cache: cache, ttl: ttl, shortTTL: shortTTL}, nil
}

// NewStatusCache creates a new StatusCache with the specified size and TTL.
// The size parameter determines the maximum number of entries that can be cached.
// When the cache is full, the least recently used entry is automatically evicted.
//
// The ttl parameter specifies how long cache entries remain valid. Expired entries
// are automatically detected and evicted on access.
//
func (sc *StatusCache) Get(domain string) *types.CacheResult {
	entry, ok := sc.cache.Get(domain)
	if !ok {
		return &types.CacheResult{Hit: false, Entry: nil, Expired: false}
	}

	now := time.Now()
	if now.After(entry.ExpiresAt) {
		sc.cache.Remove(domain)
		return &types.CacheResult{Hit: true, Entry: nil, Expired: true}
	}

	return &types.CacheResult{Hit: true, Entry: entry, Expired: false}
}

// Set stores a website response in the cache for the specified domain.
// The entry includes the response and expiration time based on the cache's TTL.
// If the cache is full, the least recently used entry is automatically evicted.
func (sc *StatusCache) Set(domain string, response *types.GatewayWebsiteResponse) {
	now := time.Now()
	entry := &types.CacheEntry{
		Response:  response,
		CachedAt:  now,
		ExpiresAt: now.Add(sc.ttl),
	}
	sc.cache.Add(domain, entry)
}

func (sc *StatusCache) SetShortTTL(domain string, response *types.GatewayWebsiteResponse) {
	now := time.Now()
	entry := &types.CacheEntry{
		Response:  response,
		CachedAt:  now,
		ExpiresAt: now.Add(sc.shortTTL),
	}
	sc.cache.Add(domain, entry)
}

// SetInvalid caches a negative result (nil response) for the specified domain.
// This provides DoS protection by remembering invalid domains and preventing
// repeated lookups that would fail anyway. The entry expires based on the cache's TTL.
func (sc *StatusCache) SetInvalid(domain string) {
	sc.Set(domain, nil)
}

// SetError caches an error result for the specified domain.
// The error is preserved so that discriminated error handling (e.g. ErrNotFound vs ErrGone)
// works consistently across cache hits and misses.
func (sc *StatusCache) SetError(domain string, err error) {
	now := time.Now()
	entry := &types.CacheEntry{
		Err:       err,
		CachedAt:  now,
		ExpiresAt: now.Add(sc.ttl),
	}
	sc.cache.Add(domain, entry)
}

func (sc *StatusCache) SetErrorShortTTL(domain string, err error) {
	now := time.Now()
	entry := &types.CacheEntry{
		Err:       err,
		CachedAt:  now,
		ExpiresAt: now.Add(sc.shortTTL),
	}
	sc.cache.Add(domain, entry)
}
