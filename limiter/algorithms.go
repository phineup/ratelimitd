package limiter

import (
	"math"
	"sort"
)

// state is the per-(namespace,key,rule) data for the in-memory store.
// Only the fields used by the rule's algorithm are meaningful.
type state struct {
	start    int64   // window start (ms)
	count    int64   // requests in the current window
	prev     int64   // requests in the previous window (sliding_window)
	tokens   float64 // token bucket
	ts       int64   // token bucket last refill (ms); 0 = no state
	log      []int64 // sliding_log timestamps, ascending
	expireAt int64   // state can be dropped after this (ms)
}

// eps absorbs floating point noise at exact boundaries. The Lua script in
// redis_script.go must use the same value and the same formulas: the two
// implementations are kept bit-for-bit equivalent and tested against each other.
const eps = 1e-9

func max0(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

// evalRule evaluates one rule against st at time now (ms). It never mutates the
// caller's state: the returned state is what to store if the request is
// admitted by every rule of the policy.
func evalRule(st state, r Rule, now, cost int64) (ruleOut, state) {
	switch r.Algorithm {
	case FixedWindow:
		return evalFixed(st, r, now, cost)
	case SlidingWindow:
		return evalSlidingCounter(st, r, now, cost)
	case SlidingLog:
		return evalLog(st, r, now, cost)
	default:
		return evalBucket(st, r, now, cost)
	}
}

func evalFixed(s state, r Rule, now, cost int64) (ruleOut, state) {
	w := r.Window.Ms()
	if r.Anchor == AnchorFirstRequest {
		if s.start == 0 || now >= s.start+w {
			s.start, s.count = now, 0
		}
	} else {
		cs := now - now%w
		if s.start != cs {
			s.start, s.count = cs, 0
		}
	}
	o := ruleOut{remBefore: max0(r.Limit - s.count)}
	nc := s.count + cost
	if nc <= r.Limit {
		o.allowed = true
		o.remAfter = r.Limit - nc
		s.count = nc
	} else {
		o.remAfter = o.remBefore
		o.retryMs = s.start + w - now
	}
	o.resetMs = s.start + w - now
	s.expireAt = s.start + w + 1000
	return o, s
}

func evalSlidingCounter(s state, r Rule, now, cost int64) (ruleOut, state) {
	w := r.Window.Ms()
	wf := float64(w)
	cs := now - now%w
	if s.start == 0 {
		s.start = cs
	}
	if cs != s.start {
		if cs-s.start >= 2*w {
			s.prev, s.count = 0, 0
		} else {
			s.prev, s.count = s.count, 0
		}
		s.start = cs
	}
	elapsed := now - s.start
	lim := float64(r.Limit)
	est := float64(float64(s.prev)*(1-float64(elapsed)/wf)) + float64(s.count) // explicit conversion: no FMA fusion (must match Lua)
	o := ruleOut{remBefore: max0(int64(math.Floor(lim - est + eps)))}
	if est+float64(cost) <= lim+eps {
		o.allowed = true
		o.remAfter = max0(int64(math.Floor(lim - est - float64(cost) + eps)))
		s.count += cost
	} else {
		o.remAfter = o.remBefore
		if s.prev > 0 && r.Limit-s.count-cost >= 0 {
			// Wait inside the current window until the previous window's
			// weight has decayed enough.
			x := wf * (1 - float64(r.Limit-s.count-cost)/float64(s.prev))
			o.retryMs = int64(math.Ceil(x)) - elapsed
		} else {
			// The current window itself is full: wait for the next window,
			// then for this window's count (now "previous") to decay.
			rem := r.Limit - cost
			x2 := 0.0
			if s.count > 0 {
				x2 = wf * (1 - float64(rem)/float64(s.count))
			}
			if x2 < 0 {
				x2 = 0
			}
			o.retryMs = (w - elapsed) + int64(math.Ceil(x2))
		}
		if o.retryMs < 1 {
			o.retryMs = 1
		}
	}
	switch {
	case s.count > 0:
		o.resetMs = s.start + 2*w - now
	case s.prev > 0:
		o.resetMs = s.start + w - now
	}
	s.expireAt = s.start + 2*w + 1000
	return o, s
}

func evalLog(s state, r Rule, now, cost int64) (ruleOut, state) {
	w := r.Window.Ms()
	cutoff := now - w
	i := sort.Search(len(s.log), func(i int) bool { return s.log[i] > cutoff })
	log := s.log[i:]
	count := int64(len(log))
	o := ruleOut{remBefore: max0(r.Limit - count)}
	if count+cost <= r.Limit {
		o.allowed = true
		o.remAfter = r.Limit - count - cost
		for j := int64(0); j < cost; j++ {
			log = append(log, now)
		}
	} else {
		o.remAfter = o.remBefore
		// Free (count+cost-limit) entries: the last one to expire decides.
		idx := count + cost - r.Limit - 1
		o.retryMs = log[idx] + w - now
	}
	if n := len(log); n > 0 {
		o.resetMs = log[n-1] + w - now
		s.expireAt = log[n-1] + w + 1000
	}
	s.log = log
	return o, s
}

func evalBucket(s state, r Rule, now, cost int64) (ruleOut, state) {
	wf := float64(r.Window.Ms())
	capf := float64(r.Capacity())
	rate := float64(r.Limit) / wf // tokens per ms
	if s.ts == 0 {
		s.tokens, s.ts = capf, now
	} else {
		elapsed := now - s.ts
		if elapsed < 0 {
			elapsed = 0
		}
		s.tokens = math.Min(capf, s.tokens+float64(float64(elapsed)*rate))
		s.ts = now
	}
	c := float64(cost)
	o := ruleOut{remBefore: int64(math.Floor(s.tokens + eps))}
	if s.tokens+eps >= c {
		o.allowed = true
		s.tokens -= c
		if s.tokens < 0 {
			s.tokens = 0
		}
		o.remAfter = int64(math.Floor(s.tokens + eps))
	} else {
		o.remAfter = o.remBefore
		o.retryMs = int64(math.Ceil((c - s.tokens) / rate))
	}
	o.resetMs = int64(math.Ceil((capf - s.tokens) / rate))
	s.expireAt = now + int64(math.Ceil(capf/rate)) + 1000
	return o, s
}
