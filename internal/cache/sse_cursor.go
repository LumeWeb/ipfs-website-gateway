package cache

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/redis/go-redis/v9"
)

const sseCursorSuffix = "sse:cursor"

// SSECursorStore persists the last fully-processed website change high-water
// mark so the gateway can resume SSE reconciliation after a restart. When no
// Redis client is configured it degrades to an in-memory cursor for the
// process lifetime.
type SSECursorStore struct {
	redis *RedisClient

	mu  sync.Mutex
	mem int
}

// NewSSECursorStore creates a cursor store backed by the given Redis client.
// A nil client keeps the cursor in memory only.
func NewSSECursorStore(redis *RedisClient) *SSECursorStore {
	return &SSECursorStore{redis: redis}
}

// Get returns the last persisted high-water mark, or 0 if none is stored.
func (s *SSECursorStore) Get(ctx context.Context) (int, error) {
	if s.redis == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.mem, nil
	}

	val, err := s.redis.Client().Get(ctx, s.redis.Key(sseCursorSuffix)).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get SSE cursor: %w", err)
	}

	mark, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("invalid SSE cursor %q: %w", val, err)
	}
	return mark, nil
}

// Set persists the high-water mark. Storage has no expiry so it survives
// restarts within the portal's retained journal window.
func (s *SSECursorStore) Set(ctx context.Context, mark int) error {
	if s.redis == nil {
		s.mu.Lock()
		s.mem = mark
		s.mu.Unlock()
		return nil
	}

	if err := s.redis.Client().Set(ctx, s.redis.Key(sseCursorSuffix), strconv.Itoa(mark), 0).Err(); err != nil {
		return fmt.Errorf("failed to set SSE cursor: %w", err)
	}
	return nil
}
