package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisClient struct {
	client *redis.Client
	prefix string
}

func NewRedisClient(url, password string, db int, prefix string) (*RedisClient, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis URL: %w", err)
	}

	if password != "" {
		opts.Password = password
	}
	opts.DB = db

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &RedisClient{client: client, prefix: prefix}, nil
}

func (r *RedisClient) Close() error {
	return r.client.Close()
}

func (r *RedisClient) Client() *redis.Client {
	return r.client
}

func (r *RedisClient) Key(suffix string) string {
	return r.prefix + suffix
}

func newRedisClientWithClient(client *redis.Client, prefix string) *RedisClient {
	return &RedisClient{client: client, prefix: prefix}
}
