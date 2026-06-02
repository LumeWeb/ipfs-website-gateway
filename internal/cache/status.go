package cache

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gammazero/workerpool"
	"github.com/hashicorp/golang-lru/v2"
	"go.lumeweb.com/ipfs-website-gateway/internal/api"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
	"go.uber.org/zap"
)

type StatusCache struct {
	cache    *lru.Cache[string, *types.CacheEntry]
	ttl      time.Duration
	shortTTL time.Duration
	staleTTL time.Duration
	redis    *StatusPersistentCache
	pool     *workerpool.WorkerPool
	api      api.APIClient
	pending  sync.Map
	logger   *zap.Logger
}

func NewStatusCache(size int, ttl time.Duration, shortTTL time.Duration, staleTTL time.Duration, redisClient *RedisClient) (*StatusCache, error) {
	var redis *StatusPersistentCache
	var logger *zap.Logger

	if redisClient != nil {
		logger = zap.NewNop()
		redis = NewStatusPersistentCache(redisClient, logger)
	}

	return NewStatusCacheWithRedis(size, ttl, shortTTL, staleTTL, redis, logger)
}

func NewStatusCacheSimple(size int, ttl time.Duration, shortTTL time.Duration) (*StatusCache, error) {
	return NewStatusCacheWithRedis(size, ttl, shortTTL, 10*time.Minute, nil, nil)
}

func NewStatusCacheWithRedis(size int, ttl time.Duration, shortTTL time.Duration, staleTTL time.Duration, redis *StatusPersistentCache, logger *zap.Logger) (*StatusCache, error) {
	if size <= 0 {
		return nil, errors.New("cache size must be positive")
	}

	if ttl <= 0 {
		return nil, errors.New("cache TTL must be positive")
	}

	if shortTTL <= 0 {
		return nil, errors.New("cache short TTL must be positive")
	}

	if staleTTL <= 0 {
		return nil, errors.New("cache stale TTL must be positive")
	}

	cache, err := lru.New[string, *types.CacheEntry](size)
	if err != nil {
		return nil, err
	}

	sc := &StatusCache{
		cache:    cache,
		ttl:      ttl,
		shortTTL: shortTTL,
		staleTTL: staleTTL,
		redis:    redis,
		pool:     workerpool.New(4),
		logger:   logger,
	}

	if redis != nil && logger != nil {
		if err := sc.loadFromRedis(); err != nil {
			logger.Warn("failed to load status entries from redis, starting fresh", zap.Error(err))
		}
	}

	return sc, nil
}

func (sc *StatusCache) SetAPIClient(client api.APIClient) {
	sc.api = client
}

func (sc *StatusCache) Get(domain string) *types.CacheResult {
	entry, ok := sc.cache.Get(domain)
	if !ok {
		if sc.redis != nil {
			if redisEntry := sc.getFromRedis(domain); redisEntry != nil {
				statusCacheHitsTotal.Inc()
				return &types.CacheResult{Hit: true, Entry: redisEntry, Expired: false}
			}
		}
		statusCacheMissesTotal.Inc()
		return &types.CacheResult{Hit: false, Entry: nil, Expired: false}
	}

	now := time.Now()
	if now.After(entry.ExpiresAt) {
		staleDeadline := entry.ExpiresAt.Add(sc.staleTTL)
		if now.After(staleDeadline) {
			statusCacheExpiredTotal.Inc()
			return &types.CacheResult{Hit: false, Entry: nil, Expired: false}
		}

		statusCacheExpiredTotal.Inc()
		sc.revalidate(domain)
		return &types.CacheResult{Hit: true, Entry: entry, Expired: true}
	}

	statusCacheHitsTotal.Inc()
	return &types.CacheResult{Hit: true, Entry: entry, Expired: false}
}

func (sc *StatusCache) Set(domain string, response *types.GatewayWebsiteResponse) {
	now := time.Now()
	entry := &types.CacheEntry{
		Response:  response,
		CachedAt:  now,
		ExpiresAt: now.Add(sc.ttl),
	}
	sc.cache.Add(domain, entry)
	sc.persistToRedis(domain, entry)
}

func (sc *StatusCache) SetShortTTL(domain string, response *types.GatewayWebsiteResponse) {
	now := time.Now()
	entry := &types.CacheEntry{
		Response:  response,
		CachedAt:  now,
		ExpiresAt: now.Add(sc.shortTTL),
	}
	sc.cache.Add(domain, entry)
	sc.persistToRedis(domain, entry)
}

func (sc *StatusCache) SetInvalid(domain string) {
	sc.Set(domain, nil)
}

func (sc *StatusCache) SetError(domain string, err error) {
	now := time.Now()
	entry := &types.CacheEntry{
		Err:       err,
		CachedAt:  now,
		ExpiresAt: now.Add(sc.ttl),
	}
	sc.cache.Add(domain, entry)
	sc.persistToRedis(domain, entry)
}

func (sc *StatusCache) SetErrorShortTTL(domain string, err error) {
	now := time.Now()
	entry := &types.CacheEntry{
		Err:       err,
		CachedAt:  now,
		ExpiresAt: now.Add(sc.shortTTL),
	}
	sc.cache.Add(domain, entry)
	sc.persistToRedis(domain, entry)
}

func (sc *StatusCache) IsDomainActive(domain string) bool {
	result := sc.Get(domain)
	return result.Hit && !result.Expired && result.Entry != nil &&
		result.Entry.Err == nil && result.Entry.Response != nil &&
		result.Entry.Response.Status == types.StatusActive
}

func (sc *StatusCache) Close() {
	sc.pool.StopWait()
}

func (sc *StatusCache) revalidate(domain string) {
	if sc.api == nil {
		return
	}

	if _, pending := sc.pending.LoadOrStore(domain, struct{}{}); pending {
		return
	}

	sc.pool.Submit(func() {
		defer sc.pending.Delete(domain)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		website, err := sc.api.GetWebsite(ctx, domain)
		if err != nil {
			if sc.logger != nil {
				sc.logger.Debug("status revalidation failed", zap.String("domain", domain), zap.Error(err))
			}
			return
		}

		if website.Status == types.StatusPendingValidation || website.Status == types.StatusBroken {
			sc.SetShortTTL(domain, website)
		} else {
			sc.Set(domain, website)
		}
	})
}

func (sc *StatusCache) persistToRedis(domain string, entry *types.CacheEntry) {
	if sc.redis == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sc.redis.Set(ctx, domain, entry); err != nil {
		if sc.logger != nil {
			sc.logger.Debug("failed to persist status entry to redis", zap.String("domain", domain), zap.Error(err))
		}
	}
}

func (sc *StatusCache) getFromRedis(domain string) *types.CacheEntry {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entry, err := sc.redis.Get(ctx, domain)
	if err != nil {
		if sc.logger != nil {
			sc.logger.Debug("failed to get status entry from redis", zap.String("domain", domain), zap.Error(err))
		}
		return nil
	}

	if entry == nil {
		return nil
	}

	if time.Now().After(entry.ExpiresAt) {
		return nil
	}

	sc.cache.Add(domain, entry)
	return entry
}

func (sc *StatusCache) loadFromRedis() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	entries, err := sc.redis.LoadAll(ctx)
	if err != nil {
		return err
	}

	for domain, entry := range entries {
		sc.cache.Add(domain, entry)
	}

	return nil
}
