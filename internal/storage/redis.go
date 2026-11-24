package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// rateLimitScript executa INCR + PEXPIRE + PTTL atomicamente no Redis para evitar
// race conditions entre múltiplas instâncias do Gateway e garantir que nenhuma chave fique sem TTL.
var rateLimitScript = redis.NewScript(`
local current = redis.call("INCR", KEYS[1])
if current == 1 then
	redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
local ttl = redis.call("PTTL", KEYS[1])
if ttl < 0 then
	redis.call("PEXPIRE", KEYS[1], ARGV[1])
	ttl = tonumber(ARGV[1])
end
return {current, ttl}
`)

// RedisStore encapsula o pool de conexões com o Redis e expõe operações atômicas para o Gateway.
type RedisStore struct {
	client *redis.Client
}

// NewRedisStore configura o pool de conexões com o Redis e testa a conectividade inicial (Ping).
func NewRedisStore(addr, password string, db int) (*RedisStore, error) {
	if addr == "" {
		return nil, errors.New("redis address must not be empty")
	}

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &RedisStore{client: client}, nil
}

// AllowRequest executa a verificação e incremento atômico do Rate Limit para uma chave.
func (s *RedisStore) AllowRequest(ctx context.Context, key string, limit int64, window time.Duration) (bool, int64, time.Duration, error) {
	if key == "" {
		return false, 0, 0, errors.New("rate limit key must not be empty")
	}
	if limit <= 0 {
		return false, 0, 0, errors.New("rate limit must be positive")
	}
	if window <= 0 {
		return false, 0, 0, errors.New("rate limit window must be positive")
	}

	windowMs := max(window.Milliseconds(), 1)

	res, err := rateLimitScript.Run(ctx, s.client, []string{key}, windowMs).Int64Slice()
	if err != nil {
		return false, 0, 0, fmt.Errorf("execute rate limit script: %w", err)
	}
	if len(res) != 2 {
		return false, 0, 0, fmt.Errorf("unexpected rate limit script result length: %d", len(res))
	}

	current := res[0]
	ttlMs := max(res[1], 0)

	allowed := current <= limit
	remaining := max(limit-current, 0)
	resetAfter := time.Duration(ttlMs) * time.Millisecond

	return allowed, remaining, resetAfter, nil
}

// Close encerra o pool de conexões com o Redis de forma limpa durante o Graceful Shutdown.
func (s *RedisStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}
