package limiter

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These tests talk to a real Redis: $RATELIMITD_TEST_REDIS (a redis:// URL) if
// set, otherwise a throwaway redis-server started from PATH. They are skipped
// when neither is available.

var redisURL string

func TestMain(m *testing.M) {
	url := os.Getenv("RATELIMITD_TEST_REDIS")
	var cleanup func()
	if url == "" {
		if path, err := exec.LookPath("redis-server"); err == nil {
			url, cleanup = startRedis(path)
		}
	}
	redisURL = url
	code := m.Run()
	if cleanup != nil {
		cleanup()
	}
	os.Exit(code)
}

func startRedis(path string, extra ...string) (url string, cleanup func()) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	args := append([]string{"--port", fmt.Sprint(port), "--bind", "127.0.0.1", "--save", "", "--appendonly", "no"}, extra...)
	cmd := exec.Command(path, args...)
	if err := cmd.Start(); err != nil {
		return "", nil
	}
	cleanup = func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 100; i++ {
		if c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
			c.Close()
			return "redis://" + addr, cleanup
		}
		time.Sleep(50 * time.Millisecond)
	}
	cleanup()
	return "", nil
}

func newTestRedis(t *testing.T) *RedisStore {
	t.Helper()
	if redisURL == "" {
		t.Skip("no redis available (install redis-server or set RATELIMITD_TEST_REDIS)")
	}
	s, err := NewRedisStore(redisURL, fmt.Sprintf("t%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRedisBasicsRealClock(t *testing.T) {
	s := newTestRedis(t)
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	rules := mustRules(t,
		Rule{Name: "session", Algorithm: FixedWindow, Anchor: AnchorFirstRequest, Limit: 2, Window: Duration(5 * time.Hour)},
		Rule{Name: "weekly", Algorithm: FixedWindow, Anchor: AnchorFirstRequest, Limit: 10, Window: Duration(7 * 24 * time.Hour)},
	)
	for i := 0; i < 2; i++ {
		if res := allow(t, s, rules, "user1", 1); !res.Allowed {
			t.Fatalf("req %d: %+v", i, res)
		}
	}
	res := allow(t, s, rules, "user1", 1)
	if res.Allowed || res.LimitedBy != "session" || res.Rules[1].Remaining != 8 {
		t.Fatalf("want session denial, weekly untouched: %+v", res)
	}
	if res.RetryAfterMs < 4*60*60*1000 || res.RetryAfterMs > 5*60*60*1000 {
		t.Fatalf("retry should be just under 5h, got %dms", res.RetryAfterMs)
	}
	if !allow(t, s, rules, "user2", 1).Allowed {
		t.Fatal("other key must be independent")
	}
	if err := s.Reset(ctx, Request{Namespace: "p", Key: "user1", Rules: rules}); err != nil {
		t.Fatal(err)
	}
	if res := allow(t, s, rules, "user1", 1); !res.Allowed || res.Rules[0].Remaining != 1 {
		t.Fatalf("after reset: %+v", res)
	}
}

func TestRedisConcurrentAtomic(t *testing.T) {
	s := newTestRedis(t)
	rules := mustRules(t,
		Rule{Name: "a", Algorithm: FixedWindow, Anchor: AnchorFirstRequest, Limit: 300, Window: Duration(time.Hour)},
		Rule{Name: "b", Algorithm: SlidingLog, Limit: 1000, Window: Duration(time.Hour)},
	)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				res, err := s.Allow(context.Background(), Request{Namespace: "p", Key: "shared", Rules: rules, Cost: 1, Consume: true})
				if err != nil {
					t.Error(err)
					return
				}
				if res.Allowed {
					allowed.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 300 {
		t.Fatalf("exactly 300 of 1000 must pass, got %d", allowed.Load())
	}
	res, _ := s.Allow(context.Background(), Request{Namespace: "p", Key: "shared", Rules: rules, Cost: 1})
	if res.Rules[1].Remaining != 700 {
		t.Fatalf("denied requests leaked into rule b: %+v", res.Rules[1])
	}
}

func TestRedisScriptCacheFlushFallsBackToEval(t *testing.T) {
	s := newTestRedis(t)
	ctx := context.Background()
	rules := mustRules(t, Rule{Name: "r", Limit: 5, Window: Duration(time.Hour)})
	allow(t, s, rules, "k", 1)
	if _, err := s.c.do(ctx, "SCRIPT", "FLUSH"); err != nil {
		t.Fatal(err)
	}
	if res := allow(t, s, rules, "k", 1); !res.Allowed || res.Remaining != 3 {
		t.Fatalf("after SCRIPT FLUSH: %+v", res)
	}
}

func TestRedisReconnectsAfterServerClosedIdleConnections(t *testing.T) {
	s := newTestRedis(t)
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil { // leaves one idle pooled connection
		t.Fatal(err)
	}
	killer, err := newRespClient(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	defer killer.close()
	if _, err := killer.do(ctx, "CLIENT", "KILL", "TYPE", "normal", "SKIPME", "yes"); err != nil {
		t.Fatal(err)
	}
	rules := mustRules(t, Rule{Name: "r", Limit: 5, Window: Duration(time.Hour)})
	res, err := s.Allow(ctx, Request{Namespace: "p", Key: "k", Rules: rules, Cost: 1, Consume: true})
	if err != nil || !res.Allowed || res.Remaining != 4 {
		t.Fatalf("stale connection must be retried transparently, and counted exactly once: %+v %v", res, err)
	}
}

func TestRedisUnreachableReturnsError(t *testing.T) {
	s, err := NewRedisStore("redis://127.0.0.1:1/0", "x")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rules := mustRules(t, Rule{Name: "r", Limit: 5, Window: Duration(time.Hour)})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := s.Allow(ctx, Request{Namespace: "p", Key: "k", Rules: rules, Cost: 1, Consume: true}); err == nil {
		t.Fatal("expected an error")
	}
	if err := s.Ping(ctx); err == nil {
		t.Fatal("expected ping to fail")
	}
}

func TestRedisAuthAndDB(t *testing.T) {
	path, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("no redis-server")
	}
	url, cleanup := startRedis(path, "--requirepass", "s3cret")
	if url == "" {
		t.Skip("could not start redis")
	}
	defer cleanup()
	ctx := context.Background()

	bad, _ := NewRedisStore(url, "x")
	if err := bad.Ping(ctx); err == nil {
		t.Fatal("ping without password must fail")
	}
	bad.Close()

	for _, u := range []string{
		"redis://:s3cret@" + url[len("redis://"):] + "/3",
		"redis://s3cret@" + url[len("redis://"):] + "/3", // password-only shorthand
		"redis://default:s3cret@" + url[len("redis://"):] + "/3",
	} {
		good, err := NewRedisStore(u, "x")
		if err != nil {
			t.Fatal(err)
		}
		if err := good.Ping(ctx); err != nil {
			t.Fatalf("%s: %v", u, err)
		}
		rules := mustRules(t, Rule{Name: "r", Limit: 5, Window: Duration(time.Hour)})
		if res := allow(t, good, rules, "k"+fmt.Sprint(len(u)), 1); !res.Allowed {
			t.Fatalf("%s: %+v", u, res)
		}
		good.Close()
	}
}

func TestParseRedisURL(t *testing.T) {
	c, err := newRespClient("rediss://user:pw@cache.example.com/2")
	if err != nil {
		t.Fatal(err)
	}
	if c.addr != "cache.example.com:6379" || c.username != "user" || c.password != "pw" || c.db != 2 || c.tlsConf == nil {
		t.Fatalf("%+v", c)
	}
	for _, bad := range []string{"http://x", "redis://h/notanumber", "://"} {
		if _, err := newRespClient(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
	if _, err := NewRedisStore("redis://h", "bad{prefix}"); err == nil {
		t.Error("prefix with braces should fail")
	}
}

// TestRedisMatchesMemory drives both stores with the same randomized request
// stream on one fake clock and requires identical results (allowed, remaining,
// retry_after, reset_after, limiting rule) for every algorithm and combination.
func TestRedisMatchesMemory(t *testing.T) {
	s := newTestRedis(t)
	ms := Duration(time.Millisecond)
	sec := Duration(time.Second)
	sets := map[string][]Rule{
		"fixed_clock":       {{Name: "r", Algorithm: FixedWindow, Limit: 5, Window: 10 * sec}},
		"fixed_anchored":    {{Name: "r", Algorithm: FixedWindow, Anchor: AnchorFirstRequest, Limit: 4, Window: 7 * sec}},
		"sliding_counter":   {{Name: "r", Algorithm: SlidingWindow, Limit: 6, Window: 10 * sec}},
		"sliding_log":       {{Name: "r", Algorithm: SlidingLog, Limit: 5, Window: 10 * sec}},
		"bucket_burst":      {{Name: "r", Algorithm: TokenBucket, Limit: 6, Window: 10 * sec, Burst: 4}},
		"bucket_noburst":    {{Name: "r", Algorithm: TokenBucket, Limit: 7, Window: 3 * sec}},
		"bucket_fractional": {{Name: "r", Algorithm: TokenBucket, Limit: 3, Window: 7 * sec * 1000 / 1000, Burst: 2}},
		"subsecond_window":  {{Name: "r", Algorithm: SlidingWindow, Limit: 3, Window: 250 * ms}},
		"claude_style": {
			{Name: "session_5h", Algorithm: FixedWindow, Anchor: AnchorFirstRequest, Limit: 8, Window: 60 * sec},
			{Name: "weekly", Algorithm: FixedWindow, Anchor: AnchorFirstRequest, Limit: 20, Window: 300 * sec},
			{Name: "burst", Algorithm: TokenBucket, Limit: 3, Window: sec, Burst: 5},
			{Name: "exact", Algorithm: SlidingLog, Limit: 12, Window: 30 * sec},
			{Name: "smooth", Algorithm: SlidingWindow, Limit: 10, Window: 20 * sec},
		},
	}
	for name, raw := range sets {
		raw := raw
		t.Run(name, func(t *testing.T) {
			rules := mustRules(t, raw...)
			clock := &fakeClock{ms: base}
			mem := newMemoryStore(clock.now)
			s.nowOverride = clock.now
			s.prefix = fmt.Sprintf("par%d", time.Now().UnixNano())
			rng := rand.New(rand.NewSource(int64(len(name)) * 7919))
			ctx := context.Background()
			maxCost := int64(3)
			for _, r := range rules {
				if r.Capacity() < maxCost {
					maxCost = r.Capacity()
				}
			}
			for step := 0; step < 600; step++ {
				switch n := rng.Intn(10); {
				case n < 5:
					clock.ms += int64(rng.Intn(500))
				case n < 8:
					clock.ms += int64(rng.Intn(3000))
				case n == 8:
					clock.ms += int64(rng.Intn(15000))
				default:
					clock.ms += int64(rng.Intn(400000))
				}
				req := Request{
					Namespace: "p",
					Key:       []string{"a", "b"}[rng.Intn(2)],
					Rules:     rules,
					Cost:      1 + int64(rng.Intn(int(maxCost))),
					Consume:   rng.Intn(5) != 0,
				}
				want, err := mem.Allow(ctx, req)
				if err != nil {
					t.Fatal(err)
				}
				got, err := s.Allow(ctx, req)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(want, got) {
					t.Fatalf("step %d (t=+%dms, key=%s cost=%d consume=%v) diverged\nmemory: %+v\nredis:  %+v",
						step, clock.ms-base, req.Key, req.Cost, req.Consume, want, got)
				}
			}
		})
	}
}
