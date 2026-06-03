package namesys

import (
	"context"
	"time"

	ds "github.com/ipfs/go-datastore"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/ipfs/boxo/namesys"
	"github.com/ipfs/boxo/path"
	"go.lumeweb.com/ipfs-website-gateway/internal/cache"
	"go.uber.org/zap"
)

type EvictFunc func(key string)

type IPNSStore struct {
	stale       *lru.Cache[string, staleEntry]
	freshTTL    time.Duration
	timeout     time.Duration
	logger      *zap.Logger
	onEvict     EvictFunc
	redisStale  *cache.IPNSStaleStore
	ds          ds.Datastore
}

func NewIPNSStore(redisClient *cache.RedisClient, freshTTL time.Duration, timeout time.Duration, maxSize int, logger *zap.Logger) (*IPNSStore, error) {
	if maxSize <= 0 {
		maxSize = 128
	}

	s := &IPNSStore{
		ds:       ds.NewMapDatastore(),
		freshTTL: freshTTL,
		timeout:  timeout,
		logger:   logger.Named("ipns-store"),
	}

	staleCache, err := lru.NewWithEvict[string, staleEntry](maxSize, func(key string, _ staleEntry) {
		if s.onEvict != nil {
			s.onEvict(key)
		}
	})
	if err != nil {
		return nil, err
	}
	s.stale = staleCache

	if redisClient != nil {
		redisStale := cache.NewIPNSStaleStore(redisClient, logger)
		s.redisStale = redisStale

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		entries, err := redisStale.LoadAllStale(ctx)
		if err != nil {
			logger.Warn("failed to load stale entries from Redis, starting fresh", zap.Error(err))
		} else {
			loaded := 0
			for key, se := range entries {
				p, err := path.NewPath(se.Path)
				if err != nil {
					continue
				}
				s.stale.Add(key, staleEntry{
					result: namesys.Result{
						Path:    p,
						TTL:     time.Duration(se.TTL),
						LastMod: time.Unix(0, se.LastMod),
					},
					cachedAt: time.Unix(0, se.CachedAt),
				})
				loaded++
			}
			logger.Info("loaded entries from Redis", zap.Int("count", loaded))
		}
	}

	logger.Info("initialized",
		zap.Int("max_size", maxSize),
		zap.Duration("fresh_ttl", freshTTL),
		zap.Duration("timeout", timeout),
		zap.Bool("redis_enabled", redisClient != nil),
	)

	return s, nil
}

func (s *IPNSStore) Datastore() ds.Datastore {
	return s.ds
}

func (s *IPNSStore) SetOnEvict(fn EvictFunc) {
	s.onEvict = fn
}

func (s *IPNSStore) Keys() []string {
	return s.stale.Keys()
}

func (s *IPNSStore) GetStale(key string) (staleEntry, bool) {
	entry, ok := s.stale.Get(key)
	if !ok {
		return staleEntry{}, false
	}
	return entry, true
}

func (s *IPNSStore) PutStale(key string, entry staleEntry) {
	s.stale.Add(key, entry)

	if s.redisStale != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		se := cache.StaleEntry{
			Path:     entry.result.Path.String(),
			TTL:      int64(entry.result.TTL),
			LastMod:  entry.result.LastMod.UnixNano(),
			CachedAt: entry.cachedAt.UnixNano(),
		}
		if err := s.redisStale.PutStale(ctx, key, se); err != nil {
			s.logger.Debug("failed to persist stale entry to Redis", zap.Error(err))
		}
	}
}

func (s *IPNSStore) DeleteStale(key string) {
	evicted := s.stale.Remove(key)
	if !evicted {
		return
	}

	if s.redisStale != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.redisStale.DeleteStale(ctx, key); err != nil {
			s.logger.Debug("failed to delete stale entry from Redis", zap.Error(err))
		}
	}
}

func (s *IPNSStore) Close() error {
	if s.redisStale != nil {
		if err := s.redisStale.Close(); err != nil {
			s.logger.Debug("failed to close Redis stale store", zap.Error(err))
		}
	}
	return nil
}
