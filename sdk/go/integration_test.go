package ratelimitd

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// Integration tests run against a real ratelimitd and are skipped unless
// RATELIMITD_URL is set. Start one with the shared SDK test config:
//
//	go run ./cmd/ratelimitd -config sdk/test-config.json
//	RATELIMITD_URL=http://127.0.0.1:18080 go test ./...
func integrationClient(t *testing.T) *Client {
	t.Helper()
	url := os.Getenv("RATELIMITD_URL")
	if url == "" {
		t.Skip("RATELIMITD_URL not set")
	}
	return New(url, WithToken(os.Getenv("RATELIMITD_TOKEN")))
}

func TestIntegrationInlineRules(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()
	key := fmt.Sprintf("go-%d", time.Now().UnixNano())
	rules := []Rule{{Algorithm: "fixed_window", Limit: 2, Window: "1m"}}

	if err := c.Healthy(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		res, err := c.Check(ctx, CheckRequest{Key: key, Rules: rules})
		if err != nil || !res.Allowed || !res.Consumed {
			t.Fatalf("request %d: %+v %v", i, res, err)
		}
	}
	res, err := c.Check(ctx, CheckRequest{Key: key, Rules: rules})
	if err != nil || res.Allowed || res.RetryAfterMs <= 0 {
		t.Fatalf("expected denial: %+v %v", res, err)
	}
	if err := c.Reset(ctx, CheckRequest{Key: key, Rules: rules}); err != nil {
		t.Fatal(err)
	}
	res, err = c.Check(ctx, CheckRequest{Key: key, Rules: rules, Peek: true})
	if err != nil || !res.Allowed || res.Consumed {
		t.Fatalf("peek after reset: %+v %v", res, err)
	}
}

func TestIntegrationValidationError(t *testing.T) {
	c := integrationClient(t)
	_, err := c.Check(context.Background(), CheckRequest{
		Key:   "k",
		Rules: []Rule{{Algorithm: "fixed_window", Limit: 1, Window: "1m"}},
		Cost:  5,
	})
	if !IsStatus(err, 422) {
		t.Fatalf("want 422, got %v", err)
	}
	_, err = c.Check(context.Background(), CheckRequest{Key: "k", Policy: "does-not-exist"})
	if !IsStatus(err, 404) {
		t.Fatalf("want 404, got %v", err)
	}
}

func TestIntegrationPolicies(t *testing.T) {
	c := integrationClient(t)
	ps, err := c.Policies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range ps {
		found = found || p.Name == "sdk_test"
	}
	if !found {
		t.Fatalf("sdk_test policy missing: %+v", ps)
	}
}
