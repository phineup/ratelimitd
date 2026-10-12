package limiter

import (
	"context"
	"hash/fnv"
	"sync"
	"time"
)

const numShards = 64

// MemoryStore keeps all state in process memory. It is the fastest option and
// is right for a single sidecar next to a single app instance; use RedisStore
// when several limiter instances must share counters.
type MemoryStore struct {
	shards [numShards]memShard
	now    func() int64 // unix ms
	stop   chan struct{}
	once   sync.Once
}

type memShard struct {
	mu sync.Mutex
	m  map[string]state
}

// NewMemoryStore creates a store and starts a janitor that evicts expired keys.
func NewMemoryStore() *MemoryStore {
	s := newMemoryStore(func() int64 { return time.Now().UnixMilli() })
	s.stop = make(chan struct{})
	go s.janitor(time.Minute)
	return s
}

func newMemoryStore(now func() int64) *MemoryStore {
	s := &MemoryStore{now: now}
	for i := range s.shards {
		s.shards[i].m = make(map[string]state)
	}
	return s
}

func (s *MemoryStore) shardFor(key string) *memShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &s.shards[h.Sum32()%numShards]
}

func memKey(req Request, r Rule) string {
	return req.Namespace + "\x00" + req.Key + "\x00" + r.Name + "\x00" + string(r.Algorithm)
}

func (s *MemoryStore) Allow(_ context.Context, req Request) (Result, error) {
	if err := req.validate(); err != nil {
		return Result{}, err
	}
	now := s.now()
	// All rules of one key live in one shard, so a single lock makes the
	// multi-rule decision atomic.
	sh := s.shardFor(req.Key)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	n := len(req.Rules)
	outs := make([]ruleOut, n)
	news := make([]state, n)
	keys := make([]string, n)
	for i, r := range req.Rules {
		keys[i] = memKey(req, r)
		var st state
		if cur, ok := sh.m[keys[i]]; ok && cur.expireAt > now {
			st = cur
		}
		outs[i], news[i] = evalRule(st, r, now, req.Cost)
	}
	res := summarize(req.Rules, outs, req.Consume)
	if res.Consumed {
		for i := range news {
			sh.m[keys[i]] = news[i]
		}
	}
	return res, nil
}

func (s *MemoryStore) Reset(_ context.Context, req Request) error {
	if req.Key == "" {
		return validationf("key is required")
	}
	sh := s.shardFor(req.Key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	for _, r := range req.Rules {
		delete(sh.m, memKey(req, r))
	}
	return nil
}

func (s *MemoryStore) Ping(context.Context) error { return nil }

// Close stops the janitor.
func (s *MemoryStore) Close() error {
	s.once.Do(func() {
		if s.stop != nil {
			close(s.stop)
		}
	})
	return nil
}

// Len returns the number of tracked (key, rule) states; used by tests/metrics.
func (s *MemoryStore) Len() int {
	n := 0
	for i := range s.shards {
		s.shards[i].mu.Lock()
		n += len(s.shards[i].m)
		s.shards[i].mu.Unlock()
	}
	return n
}

func (s *MemoryStore) janitor(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.sweep()
		}
	}
}

func (s *MemoryStore) sweep() {
	now := s.now()
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		for k, st := range sh.m {
			if st.expireAt <= now {
				delete(sh.m, k)
			}
		}
		sh.mu.Unlock()
	}
}
