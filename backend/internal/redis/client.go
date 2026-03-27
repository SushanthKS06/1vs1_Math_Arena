package redis

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/rs/zerolog"
)

type Client struct {
	rdb    *redis.Client
	ctx    context.Context
	logger zerolog.Logger
}

func NewClient(addr, password string, db int, logger zerolog.Logger) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx := context.Background()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &Client{
		rdb:    rdb,
		ctx:    ctx,
		logger: logger.With().Str("component", "redis").Logger(),
	}, nil
}

func (c *Client) Close() error {
	return c.rdb.Close()
}

func (c *Client) Set(key string, value interface{}, expiration time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.rdb.Set(ctx, key, value, expiration).Err()
}

func (c *Client) Get(key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.rdb.Get(ctx, key).Result()
}

func (c *Client) SetNX(key string, value interface{}, expiration time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.rdb.SetNX(ctx, key, value, expiration).Result()
}

func (c *Client) Del(keys ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.rdb.Del(ctx, keys...).Err()
}

func (c *Client) RPush(key string, values ...interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.rdb.RPush(ctx, key, values...).Err()
}

func (c *Client) LPop(key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.rdb.LPop(ctx, key).Result()
}

func (c *Client) LRange(key string, start, stop int64) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.rdb.LRange(ctx, key, start, stop).Result()
}

func (c *Client) LLen(key string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.rdb.LLen(ctx, key).Result()
}

func (c *Client) LRem(key string, count int64, value interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.rdb.LRem(ctx, key, count, value).Err()
}

func (c *Client) Exists(keys ...string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.rdb.Exists(ctx, keys...).Result()
}

func (c *Client) Expire(key string, expiration time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.rdb.Expire(ctx, key, expiration).Err()
}

func (c *Client) Pipeline() redis.Pipeliner {
	return c.rdb.Pipeline()
}

func (c *Client) Underlying() *redis.Client {
	return c.rdb
}

// Deprecated: create your own contexts with timeout where needed
func (c *Client) Context() context.Context {
	return context.Background()
}
