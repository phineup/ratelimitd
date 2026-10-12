package ratelimitd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestEncoding(t *testing.T) {
	var gotPath, gotCT, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotCT, gotAuth = r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"allowed":true,"policy":"inline","rules":[]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, WithToken("t0k"))
	_, err := c.Check(context.Background(), CheckRequest{
		Key:   "1.2.3.4",
		Rules: []Rule{{Algorithm: "token_bucket", Limit: 10, Window: "1m", Burst: 20}},
		Cost:  3,
		Peek:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/check" || gotCT != "application/json" || gotAuth != "Bearer t0k" {
		t.Fatalf("path=%q ct=%q auth=%q", gotPath, gotCT, gotAuth)
	}
	if gotBody["key"] != "1.2.3.4" || gotBody["cost"] != float64(3) || gotBody["peek"] != true {
		t.Fatalf("body: %v", gotBody)
	}
	if _, ok := gotBody["policy"]; ok {
		t.Fatalf("empty policy must be omitted: %v", gotBody)
	}
	rule := gotBody["rules"].([]any)[0].(map[string]any)
	if rule["algorithm"] != "token_bucket" || rule["burst"] != float64(20) || rule["window"] != "1m" {
		t.Fatalf("rule: %v", rule)
	}
}

func TestAllow(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"allowed":`+map[bool]string{true: "true", false: "false"}[status == 200]+`,"rules":[]}`)
	}))
	defer srv.Close()
	c := New(srv.URL)

	ok, err := c.Allow(context.Background(), "login", "u")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	status = http.StatusTooManyRequests
	ok, err = c.Allow(context.Background(), "login", "u")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		wantMsg    string
	}{
		{"json error", `{"error":"unknown policy \"x\""}`, 404, `unknown policy "x"`},
		{"plain text", "boom", 500, "boom"},
		{"empty body", "", 502, "Bad Gateway"},
		{"unprocessable", `{"error":"cost 9 exceeds capacity 5"}`, 422, "cost 9 exceeds capacity 5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			_, err := New(srv.URL).Check(context.Background(), CheckRequest{Key: "k", Policy: "x"})
			var ae *APIError
			if !errors.As(err, &ae) || ae.StatusCode != tc.status || ae.Message != tc.wantMsg {
				t.Fatalf("err = %#v", err)
			}
			if !strings.Contains(err.Error(), "HTTP") {
				t.Fatalf("Error() = %q", err.Error())
			}
			if IsStatus(err, tc.status+1) {
				t.Fatal("IsStatus matched the wrong status")
			}
		})
	}
}

func TestDegradedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"allowed":true,"policy":"p","degraded":true,"error":"store unavailable","rules":[]}`)
	}))
	defer srv.Close()
	res, err := New(srv.URL).Check(context.Background(), CheckRequest{Key: "k", Policy: "p"})
	if err != nil || !res.Allowed || !res.Degraded || res.Error != "store unavailable" {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestContextCancellation(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := New(srv.URL).Check(ctx, CheckRequest{Key: "k", Policy: "p"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
}

func TestMalformedSuccessBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "not json")
	}))
	defer srv.Close()
	if _, err := New(srv.URL).Check(context.Background(), CheckRequest{Key: "k", Policy: "p"}); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestWithHTTPClient(t *testing.T) {
	used := false
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		used = true
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
			Header:     make(http.Header),
		}, nil
	})}
	if err := New("http://example.invalid", WithHTTPClient(hc)).Healthy(context.Background()); err != nil || !used {
		t.Fatalf("used=%v err=%v", used, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
