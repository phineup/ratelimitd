// Package ratelimitd is a Go client for the ratelimitd rate limiting sidecar.
// It uses the standard library only.
package ratelimitd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Rule is an inline rate limit rule. Window uses the server syntax: "30s", "5h", "1d12h".
type Rule struct {
	Name      string `json:"name,omitempty"`
	Algorithm string `json:"algorithm,omitempty"`
	Limit     int64  `json:"limit"`
	Window    string `json:"window"`
	Burst     int64  `json:"burst,omitempty"`
	Anchor    string `json:"anchor,omitempty"`
}

// Policy is a named policy configured on the server.
type Policy struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Rules       []Rule `json:"rules"`
	Dynamic     bool   `json:"dynamic,omitempty"`
}

// CheckRequest describes one check. Set either Policy or Rules.
type CheckRequest struct {
	Key    string `json:"key"`
	Policy string `json:"policy,omitempty"`
	Rules  []Rule `json:"rules,omitempty"`
	Cost   int64  `json:"cost,omitempty"` // default 1
	Peek   bool   `json:"peek,omitempty"` // evaluate without consuming
}

// RuleResult is the outcome for a single rule.
type RuleResult struct {
	Name         string `json:"name"`
	Algorithm    string `json:"algorithm"`
	Allowed      bool   `json:"allowed"`
	Limit        int64  `json:"limit"`
	Remaining    int64  `json:"remaining"`
	RetryAfterMs int64  `json:"retry_after_ms"`
	ResetAfterMs int64  `json:"reset_after_ms"`
}

// Result is the decision returned by Check. A denied request (HTTP 429) is
// not an error: it is returned with Allowed == false.
type Result struct {
	Allowed      bool         `json:"allowed"`
	Consumed     bool         `json:"consumed"`
	Remaining    int64        `json:"remaining"`
	RetryAfterMs int64        `json:"retry_after_ms"`
	LimitedBy    string       `json:"limited_by,omitempty"`
	Rules        []RuleResult `json:"rules"`
	Policy       string       `json:"policy"`
	// Degraded is true when the server's store was down and it failed open.
	Degraded bool   `json:"degraded,omitempty"`
	Error    string `json:"error,omitempty"`
}

// RetryAfter returns RetryAfterMs as a time.Duration.
func (r Result) RetryAfter() time.Duration {
	return time.Duration(r.RetryAfterMs) * time.Millisecond
}

// APIError is returned for non-2xx responses other than 429.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ratelimitd: HTTP %d: %s", e.StatusCode, e.Message)
}

// Client talks to a ratelimitd server. It is safe for concurrent use.
type Client struct {
	baseURL string
	token   string
	hc      *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithToken sets the bearer token sent with every request.
func WithToken(token string) Option { return func(c *Client) { c.token = token } }

// WithHTTPClient replaces the default http.Client (10s timeout).
func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.hc = hc } }

// New creates a client for the server at baseURL, e.g. "http://localhost:8080".
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		hc:      &http.Client{Timeout: 10 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Check evaluates (and by default consumes) a request against a policy or inline rules.
func (c *Client) Check(ctx context.Context, req CheckRequest) (Result, error) {
	var res Result
	status, body, err := c.do(ctx, http.MethodPost, "/v1/check", req)
	if err != nil {
		return res, err
	}
	if status != http.StatusOK && status != http.StatusTooManyRequests {
		return res, apiError(status, body)
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return res, fmt.Errorf("ratelimitd: decode response: %w", err)
	}
	return res, nil
}

// Allow is shorthand for Check with a policy and cost 1; it returns only the decision.
func (c *Client) Allow(ctx context.Context, policy, key string) (bool, error) {
	res, err := c.Check(ctx, CheckRequest{Key: key, Policy: policy})
	return res.Allowed, err
}

// Reset clears the counters for key under a policy or inline rules.
func (c *Client) Reset(ctx context.Context, req CheckRequest) error {
	status, body, err := c.do(ctx, http.MethodPost, "/v1/reset", req)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return apiError(status, body)
	}
	return nil
}

// Policies lists the policies configured on the server.
func (c *Client) Policies(ctx context.Context) ([]Policy, error) {
	status, body, err := c.do(ctx, http.MethodGet, "/v1/policies", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, apiError(status, body)
	}
	var out struct {
		Policies []Policy `json:"policies"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("ratelimitd: decode response: %w", err)
	}
	return out.Policies, nil
}

// Healthy reports whether the server and its store are up.
func (c *Client) Healthy(ctx context.Context) error {
	status, body, err := c.do(ctx, http.MethodGet, "/healthz", nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return apiError(status, body)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, in any) (int, []byte, error) {
	var rd io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rd)
	if err != nil {
		return 0, nil, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, body, err
}

func apiError(status int, body []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	msg := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		msg = e.Error
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &APIError{StatusCode: status, Message: msg}
}

// IsStatus reports whether err is an *APIError with the given HTTP status.
func IsStatus(err error, status int) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.StatusCode == status
}
