package limiter

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct{ ms int64 }

func (c *fakeClock) now() int64              { return c.ms }
func (c *fakeClock) advance(d time.Duration) { c.ms += d.Milliseconds() }

// base is divisible by every window used below, so "clock" windows start at base.
const base = int64(1_700_006_400_000) // multiple of 1h, 1m, 10s, 1s

func mustRules(t *testing.T, rules ...Rule) []Rule {
	t.Helper()
	out, err := NormalizeRules(rules)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func newTestStore() (*MemoryStore, *fakeClock) {
	c := &fakeClock{ms: base}
	return newMemoryStore(c.now), c
}

func allow(t *testing.T, s Store, rules []Rule, key string, cost int64) Result {
	t.Helper()
	res, err := s.Allow(context.Background(), Request{Namespace: "p", Key: key, Rules: rules, Cost: cost, Consume: true})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestParseDuration(t *testing.T) {
	ok := map[string]time.Duration{
		"30s":   30 * time.Second,
		"5h":    5 * time.Hour,
		"7d":    7 * 24 * time.Hour,
		"1w":    7 * 24 * time.Hour,
		"1d12h": 36 * time.Hour,
		"1.5h":  90 * time.Minute,
		"250ms": 250 * time.Millisecond,
		"1d 2h": 26 * time.Hour,
	}
	for in, want := range ok {
		got, err := ParseDuration(in)
		if err != nil || got.Std() != want {
			t.Errorf("ParseDuration(%q) = %v, %v; want %v", in, got.Std(), err, want)
		}
	}
	for _, in := range []string{"", "h", "5", "5x", "-5h", "5h-"} {
		if _, err := ParseDuration(in); err == nil {
			t.Errorf("ParseDuration(%q) should fail", in)
		}
	}
	if s := Duration(5 * time.Hour).String(); s != "5h0m0s" {
		t.Errorf("String() = %q", s)
	}
	if s := Duration(7 * 24 * time.Hour).String(); s != "1w" {
		t.Errorf("String() = %q", s)
	}
}

func TestRuleNormalize(t *testing.T) {
	bad := []Rule{
		{Limit: 0, Window: Duration(time.Second)},
		{Limit: 1, Window: 0},
		{Limit: 1, Window: Duration(time.Second), Algorithm: "nope"},
		{Limit: 1, Window: Duration(time.Second), Burst: 2},                                          // burst on fixed
		{Limit: 1, Window: Duration(time.Second), Algorithm: SlidingWindow, Anchor: "first_request"}, // anchor on sliding
		{Limit: 1, Window: Duration(time.Second), Anchor: "weird"},
		{Limit: MaxSlidingLogLimit + 1, Window: Duration(time.Second), Algorithm: SlidingLog},
		{Limit: 1, Window: Duration(time.Second), Name: "bad name!"},
	}
	for i, r := range bad {
		if err := r.Normalize(); !errors.Is(err, ErrValidation) {
			t.Errorf("case %d: want validation error, got %v", i, err)
		}
	}
	if _, err := NormalizeRules([]Rule{
		{Name: "a", Limit: 1, Window: Duration(time.Second)},
		{Name: "a", Limit: 2, Window: Duration(time.Minute)},
	}); !errors.Is(err, ErrValidation) {
		t.Errorf("duplicate names should fail, got %v", err)
	}
	r := Rule{Limit: 5, Window: Duration(time.Minute)}
	if err := r.Normalize(); err != nil || r.Algorithm != FixedWindow || r.Anchor != AnchorClock || r.Name != "fixed_window_1m0s" {
		t.Errorf("defaults wrong: %+v %v", r, err)
	}
}

func TestFixedWindowClockAligned(t *testing.T) {
	s, c := newTestStore()
	rules := mustRules(t, Rule{Name: "r", Algorithm: FixedWindow, Limit: 3, Window: Duration(time.Minute)})
	c.advance(time.Second)
	for i := 0; i < 3; i++ {
		res := allow(t, s, rules, "k", 1)
		if !res.Allowed || res.Remaining != int64(2-i) {
			t.Fatalf("req %d: %+v", i, res)
		}
		c.advance(time.Second)
	}
	res := allow(t, s, rules, "k", 1)
	if res.Allowed || res.RetryAfterMs != 56_000 || res.LimitedBy != "r" || res.Remaining != 0 {
		t.Fatalf("4th request should be denied with retry 56s: %+v", res)
	}
	c.advance(56 * time.Second) // exactly the next minute boundary
	if res := allow(t, s, rules, "k", 1); !res.Allowed || res.Remaining != 2 {
		t.Fatalf("new window should admit: %+v", res)
	}
}

func TestFixedWindowAnchoredSession5hAndWeekly(t *testing.T) {
	s, c := newTestStore()
	rules := mustRules(t,
		Rule{Name: "session_5h", Algorithm: FixedWindow, Anchor: AnchorFirstRequest, Limit: 3, Window: Duration(5 * time.Hour)},
		Rule{Name: "weekly", Algorithm: FixedWindow, Anchor: AnchorFirstRequest, Limit: 5, Window: Duration(7 * 24 * time.Hour)},
	)
	c.advance(17 * time.Minute) // first request is not on any clock boundary
	first := c.ms
	for i := 0; i < 3; i++ {
		if res := allow(t, s, rules, "u", 1); !res.Allowed {
			t.Fatalf("req %d denied: %+v", i, res)
		}
		c.advance(time.Hour)
	}
	// 3h17m after the first request: session exhausted, resets 5h after the FIRST request.
	res := allow(t, s, rules, "u", 1)
	wantRetry := first + (5 * time.Hour).Milliseconds() - c.ms
	if res.Allowed || res.LimitedBy != "session_5h" || res.RetryAfterMs != wantRetry {
		t.Fatalf("want session_5h denial retry=%d, got %+v", wantRetry, res)
	}
	// The denied request must not have consumed the weekly quota.
	if res.Rules[1].Remaining != 2 {
		t.Fatalf("weekly must be untouched by a denied request: %+v", res.Rules[1])
	}
	c.ms = first + (5 * time.Hour).Milliseconds() // new session starts
	if res := allow(t, s, rules, "u", 1); !res.Allowed || res.Rules[0].Remaining != 2 || res.Rules[1].Remaining != 1 {
		t.Fatalf("new session: %+v", res)
	}
	allow(t, s, rules, "u", 1)
	// Weekly (5) is now exhausted even though the session has quota left.
	res = allow(t, s, rules, "u", 1)
	if res.Allowed || res.LimitedBy != "weekly" {
		t.Fatalf("weekly should limit: %+v", res)
	}
	if res.RetryAfterMs <= (5 * time.Hour).Milliseconds() {
		t.Fatalf("weekly retry should be days away, got %dms", res.RetryAfterMs)
	}
}

func TestSlidingLogExact(t *testing.T) {
	s, c := newTestStore()
	rules := mustRules(t, Rule{Name: "log", Algorithm: SlidingLog, Limit: 3, Window: Duration(10 * time.Second)})
	for i := 0; i < 3; i++ {
		if !allow(t, s, rules, "k", 1).Allowed {
			t.Fatal("should allow")
		}
		c.advance(time.Second)
	}
	res := allow(t, s, rules, "k", 1) // t=3s, oldest entry (t=0) expires at 10s
	if res.Allowed || res.RetryAfterMs != 7000 {
		t.Fatalf("want retry 7000: %+v", res)
	}
	c.advance(6999 * time.Millisecond)
	if allow(t, s, rules, "k", 1).Allowed {
		t.Fatal("1ms early must still be denied")
	}
	c.advance(time.Millisecond) // t=10s: entry at 0 is exactly one window old -> expired
	if res := allow(t, s, rules, "k", 1); !res.Allowed || res.Remaining != 0 {
		t.Fatalf("should admit at the boundary: %+v", res)
	}
}

func TestSlidingWindowCounter(t *testing.T) {
	s, c := newTestStore()
	rules := mustRules(t, Rule{Name: "sw", Algorithm: SlidingWindow, Limit: 10, Window: Duration(10 * time.Second)})
	c.advance(9 * time.Second)
	for i := 0; i < 10; i++ {
		if !allow(t, s, rules, "k", 1).Allowed {
			t.Fatalf("req %d should be allowed", i)
		}
	}
	if allow(t, s, rules, "k", 1).Allowed {
		t.Fatal("11th must be denied")
	}
	c.advance(6 * time.Second) // 5s into the next window: previous weighs 50% -> 5 free slots
	for i := 0; i < 5; i++ {
		if !allow(t, s, rules, "k", 1).Allowed {
			t.Fatalf("carry-over req %d should be allowed", i)
		}
	}
	res := allow(t, s, rules, "k", 1)
	if res.Allowed || res.RetryAfterMs != 1000 {
		t.Fatalf("want denied, retry 1000ms: %+v", res)
	}
	c.advance(time.Second)
	if !allow(t, s, rules, "k", 1).Allowed {
		t.Fatal("retry_after_ms must be accurate")
	}
	// Idle for two full windows: everything forgotten.
	c.advance(25 * time.Second)
	if res := allow(t, s, rules, "k", 1); !res.Allowed || res.Remaining != 9 {
		t.Fatalf("after idle: %+v", res)
	}
}

func TestTokenBucket(t *testing.T) {
	s, c := newTestStore()
	rules := mustRules(t, Rule{Name: "tb", Algorithm: TokenBucket, Limit: 10, Window: Duration(10 * time.Second), Burst: 5})
	for i := 0; i < 5; i++ {
		if res := allow(t, s, rules, "k", 1); !res.Allowed || res.Remaining != int64(4-i) {
			t.Fatalf("burst req %d: %+v", i, res)
		}
	}
	res := allow(t, s, rules, "k", 1)
	if res.Allowed || res.RetryAfterMs != 1000 {
		t.Fatalf("want retry 1000ms (1 token/s): %+v", res)
	}
	c.advance(time.Second)
	if !allow(t, s, rules, "k", 1).Allowed {
		t.Fatal("refilled token should be usable")
	}
	c.advance(3 * time.Second)
	if res := allow(t, s, rules, "k", 3); !res.Allowed || res.Remaining != 0 {
		t.Fatalf("cost 3 after 3s idle: %+v", res)
	}
	c.advance(time.Hour) // never exceeds burst
	if res := allow(t, s, rules, "k", 1); res.Remaining != 4 {
		t.Fatalf("bucket must cap at burst: %+v", res)
	}
}

func TestMultiRuleDenialDoesNotConsumeOthers(t *testing.T) {
	s, c := newTestStore()
	rules := mustRules(t,
		Rule{Name: "minute", Limit: 1, Window: Duration(time.Minute)},
		Rule{Name: "hour", Limit: 100, Window: Duration(time.Hour)},
	)
	if !allow(t, s, rules, "k", 1).Allowed {
		t.Fatal("first should pass")
	}
	for i := 0; i < 5; i++ {
		res := allow(t, s, rules, "k", 1)
		if res.Allowed || res.LimitedBy != "minute" || res.Rules[1].Remaining != 99 {
			t.Fatalf("denied request %d leaked into the hour rule: %+v", i, res)
		}
	}
	c.advance(time.Minute)
	if res := allow(t, s, rules, "k", 1); !res.Allowed || res.Rules[1].Remaining != 98 {
		t.Fatalf("hour counter must be 2 after two admitted requests: %+v", res)
	}
}

func TestPeekDoesNotConsumeAndResetWorks(t *testing.T) {
	s, _ := newTestStore()
	rules := mustRules(t, Rule{Name: "r", Limit: 2, Window: Duration(time.Hour)})
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		res, err := s.Allow(ctx, Request{Namespace: "p", Key: "k", Rules: rules, Cost: 1, Consume: false})
		if err != nil || !res.Allowed || res.Consumed || res.Remaining != 2 {
			t.Fatalf("peek %d: %+v %v", i, res, err)
		}
	}
	allow(t, s, rules, "k", 1)
	allow(t, s, rules, "k", 1)
	if allow(t, s, rules, "k", 1).Allowed {
		t.Fatal("should be exhausted")
	}
	if err := s.Reset(ctx, Request{Namespace: "p", Key: "k", Rules: rules}); err != nil {
		t.Fatal(err)
	}
	if res := allow(t, s, rules, "k", 1); !res.Allowed || res.Remaining != 1 {
		t.Fatalf("after reset: %+v", res)
	}
}

func TestKeysAndNamespacesAreIsolated(t *testing.T) {
	s, _ := newTestStore()
	rules := mustRules(t, Rule{Name: "r", Limit: 1, Window: Duration(time.Hour)})
	ctx := context.Background()
	if !allow(t, s, rules, "alice", 1).Allowed || allow(t, s, rules, "alice", 1).Allowed {
		t.Fatal("alice should be limited after 1")
	}
	if !allow(t, s, rules, "bob", 1).Allowed {
		t.Fatal("bob has his own quota")
	}
	res, _ := s.Allow(ctx, Request{Namespace: "other", Key: "alice", Rules: rules, Cost: 1, Consume: true})
	if !res.Allowed {
		t.Fatal("another policy namespace has its own quota")
	}
}

func TestValidation(t *testing.T) {
	s, _ := newTestStore()
	rules := mustRules(t, Rule{Name: "r", Limit: 5, Window: Duration(time.Hour)})
	ctx := context.Background()
	for name, req := range map[string]Request{
		"no key":     {Namespace: "p", Rules: rules, Cost: 1},
		"zero cost":  {Namespace: "p", Key: "k", Rules: rules, Cost: 0},
		"cost>limit": {Namespace: "p", Key: "k", Rules: rules, Cost: 6},
		"no rules":   {Namespace: "p", Key: "k", Cost: 1},
		"huge key":   {Namespace: "p", Key: string(make([]byte, 300)), Rules: rules, Cost: 1},
	} {
		if _, err := s.Allow(ctx, req); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: want validation error, got %v", name, err)
		}
	}
	// Token bucket capacity is the burst, not the limit.
	tb := mustRules(t, Rule{Name: "tb", Algorithm: TokenBucket, Limit: 100, Window: Duration(time.Minute), Burst: 3})
	if _, err := s.Allow(ctx, Request{Namespace: "p", Key: "k", Rules: tb, Cost: 4}); !errors.Is(err, ErrValidation) {
		t.Errorf("cost above burst should fail, got %v", err)
	}
}

func TestExpiredStateIsEvicted(t *testing.T) {
	s, c := newTestStore()
	rules := mustRules(t, Rule{Name: "r", Limit: 5, Window: Duration(time.Minute)})
	for i := 0; i < 100; i++ {
		allow(t, s, rules, string(rune('a'+i%26))+string(rune('a'+i/26)), 1)
	}
	if s.Len() != 100 {
		t.Fatalf("len=%d", s.Len())
	}
	c.advance(2 * time.Minute)
	s.sweep()
	if s.Len() != 0 {
		t.Fatalf("sweep left %d entries", s.Len())
	}
}

func TestConcurrentExactCount(t *testing.T) {
	s := NewMemoryStore()
	defer s.Close()
	rules := mustRules(t,
		Rule{Name: "a", Algorithm: FixedWindow, Anchor: AnchorFirstRequest, Limit: 1000, Window: Duration(time.Hour)},
		Rule{Name: "b", Algorithm: SlidingLog, Limit: 5000, Window: Duration(time.Hour)},
	)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
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
	if allowed.Load() != 1000 {
		t.Fatalf("exactly 1000 of 5000 concurrent requests must pass, got %d", allowed.Load())
	}
	// Denied requests must not have consumed the sliding log rule.
	res, _ := s.Allow(context.Background(), Request{Namespace: "p", Key: "shared", Rules: rules, Cost: 1})
	if res.Rules[1].Remaining != 4000 {
		t.Fatalf("rule b should have 4000 left, has %d", res.Rules[1].Remaining)
	}
}

func BenchmarkMemoryAllowParallel(b *testing.B) {
	s := NewMemoryStore()
	defer s.Close()
	rules, _ := NormalizeRules([]Rule{
		{Name: "burst", Algorithm: TokenBucket, Limit: 1_000_000_000, Window: Duration(time.Second)},
		{Name: "day", Algorithm: FixedWindow, Limit: 1_000_000_000, Window: Duration(24 * time.Hour)},
	})
	keys := make([]string, 1024)
	for i := range keys {
		keys[i] = "user-" + string(rune('A'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('0'+i/676))
	}
	var n atomic.Uint64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			k := keys[n.Add(1)%uint64(len(keys))]
			if _, err := s.Allow(ctx, Request{Namespace: "p", Key: k, Rules: rules, Cost: 1, Consume: true}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
