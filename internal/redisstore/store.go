package redisstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Store struct {
	client *redis.Client
	prefix string
}

func Open(ctx context.Context, rawURL, prefix string) (*Store, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis configuration")
	}
	options.DialTimeout = 3 * time.Second
	options.ReadTimeout = 2 * time.Second
	options.WriteTimeout = 2 * time.Second
	options.PoolTimeout = 2 * time.Second
	options.ContextTimeoutEnabled = true
	options.MaxRetries = -1
	options.DialerRetries = 1
	options.PoolSize = 10
	options.MaxActiveConns = 10
	options.MinIdleConns = 0
	options.DisableIdentity = true
	client := redis.NewClient(options)
	s := &Store{client: client, prefix: prefix}
	if err := s.Ping(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) RecordJoin(ctx context.Context, serverID, name, tokenHash, ipHash string) error {
	const script = `redis.call('HSET',KEYS[1],'token',ARGV[1],'ip',ARGV[2]); redis.call('EXPIRE',KEYS[1],60); return 1`
	if err := s.client.Eval(ctx, script, []string{s.key("join", serverID+":"+name)}, tokenHash, ipHash).Err(); err != nil {
		return fmt.Errorf("redis session unavailable")
	}
	return nil
}

func (s *Store) ConsumeJoin(ctx context.Context, serverID, name, ipHash string) (string, error) {
	const script = `if ARGV[1] ~= '' and redis.call('HGET',KEYS[1],'ip') ~= ARGV[1] then return '' end; local t=redis.call('HGET',KEYS[1],'token'); if t then redis.call('DEL',KEYS[1]); return t end; return ''`
	value, err := s.client.Eval(ctx, script, []string{s.key("join", serverID+":"+name)}, ipHash).Text()
	if err != nil {
		return "", fmt.Errorf("redis session unavailable")
	}
	return value, nil
}

func (s *Store) Close() error { return s.client.Close() }

func (s *Store) Ping(ctx context.Context) error {
	if err := s.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis unavailable")
	}
	return nil
}

// 敏感标识只作为摘要进入 key，避免令牌、IP 或角色名出现在 Redis 运维输出中。
func (s *Store) key(kind, identity string) string {
	sum := sha256.Sum256([]byte(identity))
	return s.prefix + kind + ":" + hex.EncodeToString(sum[:])
}

func (s *Store) Allow(ctx context.Context, identity string, limit int, window time.Duration) (bool, error) {
	if limit < 1 || window < time.Millisecond {
		return false, fmt.Errorf("invalid rate limit")
	}
	const script = `local n = redis.call('INCR', KEYS[1]); if n == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end; return n`
	n, err := s.client.Eval(ctx, script, []string{s.key("rate", identity)}, window.Milliseconds()).Int64()
	if err != nil {
		return false, fmt.Errorf("redis rate limit unavailable")
	}
	return n <= int64(limit), nil
}

func (s *Store) SaveOIDC(ctx context.Context, state, value string) error {
	if err := s.client.Set(ctx, s.key("oidc", state), value, 10*time.Minute).Err(); err != nil {
		return fmt.Errorf("external login storage unavailable")
	}
	return nil
}

func (s *Store) ConsumeOIDC(ctx context.Context, state string) (string, error) {
	value, err := s.client.GetDel(ctx, s.key("oidc", state)).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("external login storage unavailable")
	}
	return value, nil
}
