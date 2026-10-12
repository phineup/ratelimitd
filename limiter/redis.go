package limiter

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// RedisStore shares counters between any number of limiter instances. Every
// decision is one Lua script call, so it is atomic and costs one round trip.
// Requires Redis >= 5 (script effects replication, TIME inside scripts).
type RedisStore struct {
	c      *respClient
	prefix string
	sha    string
	proc   string
	seq    atomic.Uint64

	nowOverride func() int64 // tests only
}

// NewRedisStore connects lazily to url (redis:// or rediss://). prefix
// namespaces all keys (default "rl").
func NewRedisStore(url, prefix string) (*RedisStore, error) {
	c, err := newRespClient(url)
	if err != nil {
		return nil, err
	}
	if prefix == "" {
		prefix = "rl"
	}
	if strings.ContainsAny(prefix, "{} \r\n") {
		return nil, errors.New("redis key prefix must not contain braces or whitespace")
	}
	sum := sha1.Sum([]byte(luaScript))
	return &RedisStore{
		c:      c,
		prefix: prefix,
		sha:    hex.EncodeToString(sum[:]),
		proc:   fmt.Sprintf("%x", time.Now().UnixNano()),
	}, nil
}

func (s *RedisStore) key(req Request, r Rule) string {
	// {key} is the hash tag: all rules of one identity map to the same slot.
	return fmt.Sprintf("%s:{%s}:%s:%s:%s", s.prefix, req.Key, req.Namespace, r.Name, r.Algorithm)
}

func (s *RedisStore) Allow(ctx context.Context, req Request) (Result, error) {
	if err := req.validate(); err != nil {
		return Result{}, err
	}
	n := len(req.Rules)
	keys := make([]any, n)
	args := make([]any, 0, 5+5*n)
	consume := 0
	if req.Consume {
		consume = 1
	}
	var nowOverride int64 // 0 = let Redis use its own clock
	if s.nowOverride != nil {
		nowOverride = s.nowOverride()
	}
	args = append(args, consume, req.Cost, fmt.Sprintf("%s-%d", s.proc, s.seq.Add(1)), nowOverride, n)
	for i, r := range req.Rules {
		keys[i] = s.key(req, r)
		anchor := 0
		if r.Anchor == AnchorFirstRequest {
			anchor = 1
		}
		args = append(args, string(r.Algorithm), r.Limit, r.Window.Ms(), r.Burst, anchor)
	}

	reply, err := s.eval(ctx, keys, args)
	if err != nil {
		return Result{}, err
	}
	arr, ok := reply.([]any)
	if !ok || len(arr) != 1+5*n {
		return Result{}, fmt.Errorf("unexpected script reply: %v", reply)
	}
	ints := make([]int64, len(arr))
	for i, v := range arr {
		iv, ok := v.(int64)
		if !ok {
			return Result{}, fmt.Errorf("unexpected script reply element %d: %v", i, v)
		}
		ints[i] = iv
	}
	outs := make([]ruleOut, n)
	for i := range outs {
		b := 1 + i*5
		outs[i] = ruleOut{
			allowed:   ints[b] == 1,
			remBefore: ints[b+1],
			remAfter:  ints[b+2],
			retryMs:   ints[b+3],
			resetMs:   ints[b+4],
		}
	}
	return summarize(req.Rules, outs, req.Consume), nil
}

// eval runs the script by SHA and falls back to EVAL (which also loads it).
func (s *RedisStore) eval(ctx context.Context, keys, args []any) (any, error) {
	build := func(cmd, script string) []any {
		a := make([]any, 0, 3+len(keys)+len(args))
		a = append(a, cmd, script, len(keys))
		a = append(a, keys...)
		return append(a, args...)
	}
	reply, err := s.c.do(ctx, build("EVALSHA", s.sha)...)
	var rerr redisError
	if errors.As(err, &rerr) && strings.HasPrefix(string(rerr), "NOSCRIPT") {
		return s.c.do(ctx, build("EVAL", luaScript)...)
	}
	return reply, err
}

func (s *RedisStore) Reset(ctx context.Context, req Request) error {
	if req.Key == "" {
		return validationf("key is required")
	}
	if len(req.Rules) == 0 {
		return nil
	}
	args := []any{"DEL"}
	for _, r := range req.Rules {
		args = append(args, s.key(req, r))
	}
	_, err := s.c.do(ctx, args...)
	return err
}

func (s *RedisStore) Ping(ctx context.Context) error {
	_, err := s.c.do(ctx, "PING")
	return err
}

func (s *RedisStore) Close() error {
	s.c.close()
	return nil
}
