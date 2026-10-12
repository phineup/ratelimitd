package ratelimitd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckAllowedAndDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		var req CheckRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Key == "denied" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"allowed":false,"retry_after_ms":1500,"limited_by":"r","policy":"p","rules":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"allowed":true,"consumed":true,"remaining":4,"policy":"p","rules":[]}`))
	}))
	defer srv.Close()
	ctx := context.Background()

	c := New(srv.URL+"/", WithToken("tok"))
	res, err := c.Check(ctx, CheckRequest{Key: "u", Policy: "p"})
	if err != nil || !res.Allowed || res.Remaining != 4 {
		t.Fatalf("allowed: %+v %v", res, err)
	}
	res, err = c.Check(ctx, CheckRequest{Key: "denied", Policy: "p"})
	if err != nil || res.Allowed || res.RetryAfter().Milliseconds() != 1500 || res.LimitedBy != "r" {
		t.Fatalf("denied: %+v %v", res, err)
	}
	if _, err = New(srv.URL).Check(ctx, CheckRequest{Key: "u"}); !IsStatus(err, 401) {
		t.Fatalf("want 401 APIError, got %v", err)
	}
}

func TestResetPoliciesHealthy(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/reset", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /v1/policies", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"policies":[{"name":"login","rules":[{"limit":5,"window":"1m"}]}]}`))
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"store unavailable"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, ctx := New(srv.URL), context.Background()

	if err := c.Reset(ctx, CheckRequest{Key: "u", Policy: "login"}); err != nil {
		t.Fatal(err)
	}
	ps, err := c.Policies(ctx)
	if err != nil || len(ps) != 1 || ps[0].Name != "login" || ps[0].Rules[0].Limit != 5 {
		t.Fatalf("policies: %+v %v", ps, err)
	}
	if err := c.Healthy(ctx); !IsStatus(err, 503) {
		t.Fatalf("healthy: %v", err)
	}
}
