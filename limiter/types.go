// Package limiter implements the rate limiting algorithms and the stores
// (in-memory and Redis) that evaluate them.
package limiter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Algorithm selects how a rule counts requests.
type Algorithm string

const (
	// FixedWindow counts requests in a window. With Anchor "clock" windows are
	// aligned to the wall clock (minute boundaries etc.). With Anchor
	// "first_request" the window starts at the first request and lasts Window,
	// which is how "5 hour session" / "weekly" quotas usually work.
	FixedWindow Algorithm = "fixed_window"
	// SlidingWindow is the weighted two-window approximation (cheap, O(1),
	// within a few percent of exact).
	SlidingWindow Algorithm = "sliding_window"
	// SlidingLog keeps every timestamp. Exact, but O(limit) memory per key.
	SlidingLog Algorithm = "sliding_log"
	// TokenBucket refills Limit tokens per Window up to Burst (default Limit).
	TokenBucket Algorithm = "token_bucket"
)

// Anchor controls where a fixed window starts.
type Anchor string

const (
	AnchorClock        Anchor = "clock"
	AnchorFirstRequest Anchor = "first_request"
)

// MaxSlidingLogLimit bounds memory use of the exact sliding log.
const MaxSlidingLogLimit = 100_000

// MaxRulesPerPolicy bounds the work done per check.
const MaxRulesPerPolicy = 16

var (
	// ErrValidation marks errors caused by bad input (HTTP 422).
	ErrValidation = errors.New("validation error")

	nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
)

func validationf(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, a...))
}

// Duration is a time.Duration that also understands "d" and "w" units
// ("5h", "7d", "1w", "1d12h") and (de)serializes as a string.
type Duration time.Duration

var unitMap = map[string]time.Duration{
	"ns": time.Nanosecond,
	"us": time.Microsecond,
	"µs": time.Microsecond,
	"ms": time.Millisecond,
	"s":  time.Second,
	"m":  time.Minute,
	"h":  time.Hour,
	"d":  24 * time.Hour,
	"w":  7 * 24 * time.Hour,
}

// ParseDuration parses strings like "30s", "5h", "7d", "1w", "1d12h".
func ParseDuration(s string) (Duration, error) {
	orig := s
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	if s == "" {
		return 0, errors.New("empty duration")
	}
	isNum := func(c byte) bool { return (c >= '0' && c <= '9') || c == '.' }
	var total float64
	for s != "" {
		i := 0
		for i < len(s) && isNum(s[i]) {
			i++
		}
		if i == 0 {
			return 0, fmt.Errorf("invalid duration %q", orig)
		}
		num, err := strconv.ParseFloat(s[:i], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", orig)
		}
		s = s[i:]
		j := 0
		for j < len(s) && !isNum(s[j]) {
			j++
		}
		u, ok := unitMap[s[:j]]
		if !ok {
			return 0, fmt.Errorf("invalid duration %q: unknown unit %q", orig, s[:j])
		}
		total += num * float64(u)
		s = s[j:]
	}
	if total > float64(math.MaxInt64) {
		return 0, fmt.Errorf("duration %q too large", orig)
	}
	return Duration(total), nil
}

func (d Duration) Std() time.Duration { return time.Duration(d) }
func (d Duration) Ms() int64          { return time.Duration(d).Milliseconds() }

func (d Duration) String() string {
	td := time.Duration(d)
	week, day := 7*24*time.Hour, 24*time.Hour
	switch {
	case td > 0 && td%week == 0:
		return fmt.Sprintf("%dw", td/week)
	case td > 0 && td%day == 0:
		return fmt.Sprintf("%dd", td/day)
	}
	return td.String()
}

func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// UnmarshalJSON accepts a string ("5h") or a number (seconds).
func (d *Duration) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		return d.UnmarshalText([]byte(s))
	}
	var secs float64
	if err := json.Unmarshal(b, &secs); err != nil {
		return fmt.Errorf("duration must be a string like \"5h\" or a number of seconds")
	}
	*d = Duration(secs * float64(time.Second))
	return nil
}

// Rule is one limit. A Policy combines several rules; a request must pass all.
type Rule struct {
	Name      string    `json:"name,omitempty"`
	Algorithm Algorithm `json:"algorithm,omitempty"`
	Limit     int64     `json:"limit"`
	Window    Duration  `json:"window"`
	// Burst is the token bucket capacity (default: Limit). token_bucket only.
	Burst int64 `json:"burst,omitempty"`
	// Anchor is "clock" (default) or "first_request". fixed_window only.
	Anchor Anchor `json:"anchor,omitempty"`
}

// Capacity is the largest cost a single request may have under this rule.
func (r Rule) Capacity() int64 {
	if r.Algorithm == TokenBucket && r.Burst > 0 {
		return r.Burst
	}
	return r.Limit
}

// Normalize fills defaults and validates the rule.
func (r *Rule) Normalize() error {
	if r.Algorithm == "" {
		r.Algorithm = FixedWindow
	}
	switch r.Algorithm {
	case FixedWindow, SlidingWindow, SlidingLog, TokenBucket:
	default:
		return validationf("unknown algorithm %q (use fixed_window, sliding_window, sliding_log, token_bucket)", r.Algorithm)
	}
	if r.Limit <= 0 {
		return validationf("limit must be > 0")
	}
	if r.Window.Ms() < 1 {
		return validationf("window must be at least 1ms")
	}
	if r.Burst < 0 {
		return validationf("burst must be >= 0")
	}
	if r.Burst != 0 && r.Algorithm != TokenBucket {
		return validationf("burst is only valid for token_bucket")
	}
	if r.Anchor == "" {
		r.Anchor = AnchorClock
	}
	if r.Anchor != AnchorClock && r.Anchor != AnchorFirstRequest {
		return validationf("anchor must be %q or %q", AnchorClock, AnchorFirstRequest)
	}
	if r.Anchor != AnchorClock && r.Algorithm != FixedWindow {
		return validationf("anchor is only valid for fixed_window")
	}
	if r.Algorithm == SlidingLog && r.Limit > MaxSlidingLogLimit {
		return validationf("sliding_log limit is capped at %d (memory is O(limit)); use sliding_window", MaxSlidingLogLimit)
	}
	if r.Name == "" {
		r.Name = fmt.Sprintf("%s_%s", r.Algorithm, r.Window)
	}
	if !nameRE.MatchString(r.Name) {
		return validationf("rule name %q must match %s", r.Name, nameRE)
	}
	return nil
}

// NormalizeRules normalizes every rule and checks names are unique.
func NormalizeRules(rules []Rule) ([]Rule, error) {
	if len(rules) == 0 {
		return nil, validationf("at least one rule is required")
	}
	if len(rules) > MaxRulesPerPolicy {
		return nil, validationf("at most %d rules per policy", MaxRulesPerPolicy)
	}
	out := make([]Rule, len(rules))
	copy(out, rules)
	seen := map[string]bool{}
	for i := range out {
		if err := out[i].Normalize(); err != nil {
			return nil, fmt.Errorf("rule %d: %w", i, err)
		}
		if seen[out[i].Name] {
			return nil, validationf("rule %d: duplicate rule name %q", i, out[i].Name)
		}
		seen[out[i].Name] = true
	}
	return out, nil
}

// Policy is a named set of rules.
type Policy struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Rules       []Rule `json:"rules"`
	// Dynamic is true for policies created through the API (not the config file).
	Dynamic bool `json:"dynamic,omitempty"`
}

// Normalize validates the policy name and rules.
func (p *Policy) Normalize() error {
	if !nameRE.MatchString(p.Name) {
		return validationf("policy name %q must match %s", p.Name, nameRE)
	}
	rules, err := NormalizeRules(p.Rules)
	if err != nil {
		return err
	}
	p.Rules = rules
	return nil
}

// Request is one decision to make.
type Request struct {
	Namespace string // policy name, or "_inline"
	Key       string // who is being limited (api key, user id, ip, ...)
	Rules     []Rule // already normalized
	Cost      int64  // how many units this request consumes (default 1)
	Consume   bool   // false = peek: evaluate without consuming
}

const maxKeyLen = 256

func (r *Request) validate() error {
	if r.Key == "" {
		return validationf("key is required")
	}
	if len(r.Key) > maxKeyLen {
		return validationf("key longer than %d bytes", maxKeyLen)
	}
	if r.Cost < 1 {
		return validationf("cost must be >= 1")
	}
	if len(r.Rules) == 0 {
		return validationf("no rules")
	}
	for _, rule := range r.Rules {
		if r.Cost > rule.Capacity() {
			return validationf("cost %d exceeds capacity %d of rule %q; the request can never succeed", r.Cost, rule.Capacity(), rule.Name)
		}
	}
	return nil
}

// RuleResult is the outcome for one rule.
type RuleResult struct {
	Name         string    `json:"name"`
	Algorithm    Algorithm `json:"algorithm"`
	Allowed      bool      `json:"allowed"`
	Limit        int64     `json:"limit"`
	Remaining    int64     `json:"remaining"`
	RetryAfterMs int64     `json:"retry_after_ms"`
	// ResetAfterMs is when this rule's quota is fully restored, computed as if
	// the request had been consumed (for sliding_window it is an upper bound).
	ResetAfterMs int64 `json:"reset_after_ms"`
}

// Result is the combined decision across all rules.
type Result struct {
	Allowed bool `json:"allowed"`
	// Consumed is true when the request was counted (allowed and not a peek).
	Consumed bool `json:"consumed"`
	// Remaining is the smallest remaining quota across rules.
	Remaining int64 `json:"remaining"`
	// RetryAfterMs is 0 when allowed, otherwise how long until every denied
	// rule would admit the request.
	RetryAfterMs int64 `json:"retry_after_ms"`
	// LimitedBy names the rule that determines RetryAfterMs when denied.
	LimitedBy string       `json:"limited_by,omitempty"`
	Rules     []RuleResult `json:"rules"`
}

// Primary returns the rule that best summarizes the result: the limiting rule
// when denied, otherwise the rule with the least remaining quota.
func (r Result) Primary() (RuleResult, bool) {
	if len(r.Rules) == 0 {
		return RuleResult{}, false
	}
	best := r.Rules[0]
	for _, rr := range r.Rules {
		if !r.Allowed && rr.Name == r.LimitedBy {
			return rr, true
		}
		if r.Allowed && rr.Remaining < best.Remaining {
			best = rr
		}
	}
	return best, true
}

// Store evaluates rules against persistent or in-memory state.
type Store interface {
	// Allow atomically evaluates every rule; the request is consumed from all
	// rules only if all of them admit it.
	Allow(ctx context.Context, req Request) (Result, error)
	// Reset forgets the state for req.Key under req.Rules.
	Reset(ctx context.Context, req Request) error
	Ping(ctx context.Context) error
	Close() error
}

// ruleOut is the raw per-rule outcome shared by all stores.
type ruleOut struct {
	allowed   bool
	remBefore int64
	remAfter  int64
	retryMs   int64
	resetMs   int64
}

func summarize(rules []Rule, outs []ruleOut, consume bool) Result {
	res := Result{Allowed: true, Remaining: math.MaxInt64}
	for _, o := range outs {
		if !o.allowed {
			res.Allowed = false
		}
	}
	res.Consumed = res.Allowed && consume
	for i, o := range outs {
		rem := o.remBefore
		if res.Consumed {
			rem = o.remAfter
		}
		if !o.allowed && o.retryMs > res.RetryAfterMs {
			res.RetryAfterMs = o.retryMs
			res.LimitedBy = rules[i].Name
		}
		if rem < res.Remaining {
			res.Remaining = rem
		}
		res.Rules = append(res.Rules, RuleResult{
			Name:         rules[i].Name,
			Algorithm:    rules[i].Algorithm,
			Allowed:      o.allowed,
			Limit:        rules[i].Limit,
			Remaining:    rem,
			RetryAfterMs: o.retryMs,
			ResetAfterMs: o.resetMs,
		})
	}
	return res
}
