package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ratelimitd/limiter"
)

func newServer(t *testing.T, store limiter.Store, tokens ...string) *httptest.Server {
	t.Helper()
	p := limiter.Policy{Name: "claude", Rules: []limiter.Rule{
		{Name: "session_5h", Algorithm: limiter.FixedWindow, Anchor: limiter.AnchorFirstRequest, Limit: 2, Window: limiter.Duration(5 * time.Hour)},
		{Name: "weekly", Algorithm: limiter.FixedWindow, Anchor: limiter.AnchorFirstRequest, Limit: 10, Window: limiter.Duration(7 * 24 * time.Hour)},
	}}
	if err := p.Normalize(); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: store, Policies: map[string]limiter.Policy{"claude": p}, Tokens: tokens, FailOpen: true}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func post(t *testing.T, ts *httptest.Server, path, token string, body any) (*http.Response, CheckResponse) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", ts.URL+path, bytes.NewReader(b))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out CheckResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestCheckAllowThenDeny(t *testing.T) {
	ts := newServer(t, limiter.NewMemoryStore())
	for i := 0; i < 2; i++ {
		resp, out := post(t, ts, "/v1/check", "", CheckRequest{Key: "u1", Policy: "claude"})
		if resp.StatusCode != 200 || !out.Allowed {
			t.Fatalf("req %d: %d %+v", i, resp.StatusCode, out)
		}
	}
	resp, out := post(t, ts, "/v1/check", "", CheckRequest{Key: "u1", Policy: "claude"})
	if resp.StatusCode != 429 || out.Allowed || out.LimitedBy != "session_5h" || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("want 429 limited by session_5h: %d %+v", resp.StatusCode, out)
	}
	if resp, _ := post(t, ts, "/v1/check", "", CheckRequest{Key: "u2", Policy: "claude"}); resp.StatusCode != 200 {
		t.Fatal("other key must be allowed")
	}
}

func TestInlineRulesPeekAndReset(t *testing.T) {
	ts := newServer(t, limiter.NewMemoryStore())
	rules := []limiter.Rule{{Algorithm: limiter.TokenBucket, Limit: 60, Window: limiter.Duration(time.Minute), Burst: 1}}
	if _, out := post(t, ts, "/v1/check", "", CheckRequest{Key: "k", Rules: rules, Peek: true}); !out.Allowed || out.Consumed {
		t.Fatalf("peek: %+v", out)
	}
	post(t, ts, "/v1/check", "", CheckRequest{Key: "k", Rules: rules})
	if resp, _ := post(t, ts, "/v1/check", "", CheckRequest{Key: "k", Rules: rules}); resp.StatusCode != 429 {
		t.Fatal("burst of 1 should deny the second request")
	}
	if resp, _ := post(t, ts, "/v1/reset", "", CheckRequest{Key: "k", Rules: rules}); resp.StatusCode != 204 {
		t.Fatalf("reset: %d", resp.StatusCode)
	}
	if resp, _ := post(t, ts, "/v1/check", "", CheckRequest{Key: "k", Rules: rules}); resp.StatusCode != 200 {
		t.Fatal("after reset should allow")
	}
}

func TestBadInput(t *testing.T) {
	ts := newServer(t, limiter.NewMemoryStore())
	cases := map[int]CheckRequest{
		404: {Key: "k", Policy: "nope"},
		422: {Key: "k"}, // no policy, no rules
		// cost above any rule's capacity can never succeed
	}
	for want, body := range cases {
		if resp, _ := post(t, ts, "/v1/check", "", body); resp.StatusCode != want {
			t.Errorf("%+v: got %d want %d", body, resp.StatusCode, want)
		}
	}
	if resp, _ := post(t, ts, "/v1/check", "", CheckRequest{Key: "k", Policy: "claude", Cost: 99}); resp.StatusCode != 422 {
		t.Errorf("cost>limit: got %d", resp.StatusCode)
	}
	if resp, _ := post(t, ts, "/v1/check", "", map[string]any{"key": "k", "bogus": 1}); resp.StatusCode != 400 {
		t.Errorf("unknown field: got %d", resp.StatusCode)
	}
}

func TestBearerAuth(t *testing.T) {
	ts := newServer(t, limiter.NewMemoryStore(), "secret")
	if resp, _ := post(t, ts, "/v1/check", "", CheckRequest{Key: "k", Policy: "claude"}); resp.StatusCode != 401 {
		t.Fatalf("no token: %d", resp.StatusCode)
	}
	if resp, _ := post(t, ts, "/v1/check", "wrong", CheckRequest{Key: "k", Policy: "claude"}); resp.StatusCode != 401 {
		t.Fatalf("wrong token: %d", resp.StatusCode)
	}
	if resp, _ := post(t, ts, "/v1/check", "secret", CheckRequest{Key: "k", Policy: "claude"}); resp.StatusCode != 200 {
		t.Fatalf("good token: %d", resp.StatusCode)
	}
	if resp, err := http.Get(ts.URL + "/healthz"); err != nil || resp.StatusCode != 200 {
		t.Fatal("healthz must not need auth")
	}
}

type brokenStore struct{ limiter.Store }

func (brokenStore) Allow(context.Context, limiter.Request) (limiter.Result, error) {
	return limiter.Result{}, errors.New("redis down")
}

func TestFailOpenAndFailClosed(t *testing.T) {
	ts := newServer(t, brokenStore{limiter.NewMemoryStore()})
	resp, out := post(t, ts, "/v1/check", "", CheckRequest{Key: "k", Policy: "claude"})
	if resp.StatusCode != 200 || !out.Allowed || !out.Degraded {
		t.Fatalf("fail-open: %d %+v", resp.StatusCode, out)
	}
	s := &Server{Store: brokenStore{limiter.NewMemoryStore()}, Policies: map[string]limiter.Policy{"claude": {Name: "claude", Rules: []limiter.Rule{{Name: "r", Algorithm: "fixed_window", Limit: 1, Window: limiter.Duration(time.Hour)}}}}}
	closed := httptest.NewServer(s.Handler())
	defer closed.Close()
	if resp, _ := post(t, closed, "/v1/check", "", CheckRequest{Key: "k", Policy: "claude"}); resp.StatusCode != 503 {
		t.Fatalf("fail-closed: %d", resp.StatusCode)
	}
}
