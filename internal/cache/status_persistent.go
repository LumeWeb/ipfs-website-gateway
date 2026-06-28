package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
	"go.uber.org/zap"
)

const statusSuffix = "status:"

type persistentCacheEntry struct {
	Response  *types.GatewayWebsiteResponse `json:"r,omitempty"`
	ErrString string                        `json:"e,omitempty"`
	CachedAt  time.Time                     `json:"c"`
	ExpiresAt time.Time                     `json:"x"`
}

type StatusPersistentCache struct {
	redis  *RedisClient
	logger *zap.Logger
}

func NewStatusPersistentCache(redis *RedisClient, logger *zap.Logger) *StatusPersistentCache {
	return &StatusPersistentCache{
		redis:  redis,
		logger: logger.Named("status-persistent"),
	}
}

func (spc *StatusPersistentCache) Get(ctx context.Context, domain string) (*types.CacheEntry, error) {
	redisKey := spc.redis.Key(statusSuffix + domain)
	data, err := spc.redis.Client().Get(ctx, redisKey).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get status entry: %w", err)
	}

	var pe persistentCacheEntry
	if err := json.Unmarshal(data, &pe); err != nil {
		return nil, fmt.Errorf("failed to unmarshal status entry: %w", err)
	}

	entry := &types.CacheEntry{
		Response:  pe.Response,
		CachedAt:  pe.CachedAt,
		ExpiresAt: pe.ExpiresAt,
	}

	if pe.ErrString != "" {
		entry.Err = fmt.Errorf("%s", pe.ErrString)
	}

	return entry, nil
}

func (spc *StatusPersistentCache) Set(ctx context.Context, domain string, entry *types.CacheEntry) error {
	pe := persistentCacheEntry{
		Response:  entry.Response,
		CachedAt:  entry.CachedAt,
		ExpiresAt: entry.ExpiresAt,
	}

	if entry.Err != nil {
		pe.ErrString = entry.Err.Error()
	}

	data, err := json.Marshal(pe)
	if err != nil {
		return fmt.Errorf("failed to marshal status entry: %w", err)
	}

	ttl := time.Until(entry.ExpiresAt).Round(time.Second)
	if ttl <= 0 {
		return nil
	}

	redisKey := spc.redis.Key(statusSuffix + domain)
	if err := spc.redis.Client().Set(ctx, redisKey, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to set status entry: %w", err)
	}

	return nil
}

func (spc *StatusPersistentCache) Delete(ctx context.Context, domain string) error {
	redisKey := spc.redis.Key(statusSuffix + domain)
	if err := spc.redis.Client().Del(ctx, redisKey).Err(); err != nil {
		return fmt.Errorf("failed to delete status entry: %w", err)
	}
	return nil
}

func (spc *StatusPersistentCache) LoadAll(ctx context.Context) (map[string]*types.CacheEntry, error) {
	var cursor uint64
	entries := make(map[string]*types.CacheEntry)
	scanPattern := spc.redis.Key(statusSuffix + "*")
	stripPrefix := spc.redis.Key(statusSuffix)

	for {
		keys, nextCursor, err := spc.redis.Client().Scan(ctx, cursor, scanPattern, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan status entries: %w", err)
		}

		for _, redisKey := range keys {
			data, err := spc.redis.Client().Get(ctx, redisKey).Bytes()
			if err != nil {
				if err == redis.Nil {
					continue
				}
				spc.logger.Debug("failed to load status entry", zap.String("key", redisKey), zap.Error(err))
				continue
			}

			var pe persistentCacheEntry
			if err := json.Unmarshal(data, &pe); err != nil {
				spc.logger.Debug("failed to unmarshal status entry", zap.String("key", redisKey), zap.Error(err))
				continue
			}

			if time.Now().After(pe.ExpiresAt) {
				continue
			}

			entry := &types.CacheEntry{
				Response:  pe.Response,
				CachedAt:  pe.CachedAt,
				ExpiresAt: pe.ExpiresAt,
			}

			if pe.ErrString != "" {
				entry.Err = fmt.Errorf("%s", pe.ErrString)
			}

			domain := redisKey[len(stripPrefix):]
			entries[domain] = entry
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	spc.logger.Info("loaded status entries from redis", zap.Int("count", len(entries)))
	return entries, nil
}
