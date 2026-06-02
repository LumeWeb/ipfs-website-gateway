package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const ipnsStaleSuffix = "ipns:stale:"

type StaleEntry struct {
	Path     string `json:"p"`
	TTL      int64  `json:"t"`
	LastMod  int64  `json:"m"`
	CachedAt int64  `json:"c"`
}

type IPNSStaleStore struct {
	redis    *RedisClient
	logger   *zap.Logger
	freshTTL time.Duration
}

func NewIPNSStaleStore(redis *RedisClient, freshTTL time.Duration, logger *zap.Logger) *IPNSStaleStore {
	return &IPNSStaleStore{
		redis:    redis,
		freshTTL: freshTTL,
		logger:   logger.Named("ipns-stale"),
	}
}

func (s *IPNSStaleStore) PutStale(ctx context.Context, key string, entry StaleEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal stale entry: %w", err)
	}

	redisKey := s.redis.Key(ipnsStaleSuffix + key)
	if err := s.redis.Client().Set(ctx, redisKey, data, s.freshTTL).Err(); err != nil {
		return fmt.Errorf("failed to put stale entry: %w", err)
	}

	return nil
}

func (s *IPNSStaleStore) GetStale(ctx context.Context, key string) (StaleEntry, error) {
	redisKey := s.redis.Key(ipnsStaleSuffix + key)
	data, err := s.redis.Client().Get(ctx, redisKey).Bytes()
	if err != nil {
		if err == redis.Nil {
			return StaleEntry{}, fmt.Errorf("stale entry not found: %s", key)
		}
		return StaleEntry{}, fmt.Errorf("failed to get stale entry: %w", err)
	}

	var entry StaleEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return StaleEntry{}, fmt.Errorf("failed to unmarshal stale entry: %w", err)
	}

	return entry, nil
}

func (s *IPNSStaleStore) DeleteStale(ctx context.Context, key string) error {
	redisKey := s.redis.Key(ipnsStaleSuffix + key)
	if err := s.redis.Client().Del(ctx, redisKey).Err(); err != nil {
		return fmt.Errorf("failed to delete stale entry: %w", err)
	}
	return nil
}

func (s *IPNSStaleStore) LoadAllStale(ctx context.Context) (map[string]StaleEntry, error) {
	var cursor uint64
	entries := make(map[string]StaleEntry)
	scanPattern := s.redis.Key(ipnsStaleSuffix + "*")
	stripPrefix := s.redis.Key(ipnsStaleSuffix)

	for {
		keys, nextCursor, err := s.redis.Client().Scan(ctx, cursor, scanPattern, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan stale entries: %w", err)
		}

		for _, redisKey := range keys {
			data, err := s.redis.Client().Get(ctx, redisKey).Bytes()
			if err != nil {
				if err == redis.Nil {
					continue
				}
				s.logger.Debug("failed to load stale entry", zap.String("key", redisKey), zap.Error(err))
				continue
			}

			var entry StaleEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				s.logger.Debug("failed to unmarshal stale entry", zap.String("key", redisKey), zap.Error(err))
				continue
			}

			key := redisKey[len(stripPrefix):]
			entries[key] = entry
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	s.logger.Info("loaded stale entries from redis", zap.Int("count", len(entries)))
	return entries, nil
}

func (s *IPNSStaleStore) Close() error {
	return nil
}
