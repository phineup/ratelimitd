// Package server exposes the limiter over HTTP.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"ratelimitd/limiter"
)

type Server struct {
	Store         limiter.Store
	Policies      map[string]limiter.Policy // already normalized
	DefaultPolicy string
	Tokens        []string // bearer tokens; empty = no auth
	FailOpen      bool     // allow requests when the store is down
}

type CheckRequest struct {
	Key    string         `json:"key"`
	Policy string         `json:"policy,omitempty"`
	Rules  []limiter.Rule `json:"rules,omitempty"` // inline rules instead of a policy
	Cost   int64          `json:"cost,omitempty"`  // default 1
	Peek   bool           `json:"peek,omitempty"`  // evaluate without consuming
}

type CheckResponse struct {
	limiter.Result
	Policy   string `json:"policy"`
	Degraded bool   `json:"degraded,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Store.Ping(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "store unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("POST /v1/check", s.auth(http.HandlerFunc(s.check)))
	mux.Handle("POST /v1/reset", s.auth(http.HandlerFunc(s.reset)))
	mux.Handle("GET /v1/policies", s.auth(http.HandlerFunc(s.list)))
	return mux
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.Tokens) > 0 {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			ok := false
			for _, t := range s.Tokens {
				if subtle.ConstantTimeCompare([]byte(got), []byte(t)) == 1 {
					ok = true
				}
			}
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// resolve turns a request into (namespace, normalized rules).
func (s *Server) resolve(policy string, inline []limiter.Rule) (string, []limiter.Rule, int, error) {
	if policy != "" && len(inline) > 0 {
		return "", nil, http.StatusUnprocessableEntity, errors.New("use either policy or rules, not both")
	}
	if len(inline) > 0 {
		rules, err := limiter.NormalizeRules(inline)
		if err != nil {
			return "", nil, http.StatusUnprocessableEntity, err
		}
		return "_inline", rules, 0, nil
	}
	if policy == "" {
		policy = s.DefaultPolicy
	}
	if policy == "" {
		return "", nil, http.StatusUnprocessableEntity, errors.New("policy or rules is required")
	}
	p, ok := s.Policies[policy]
	if !ok {
		return "", nil, http.StatusNotFound, fmt.Errorf("unknown policy %q", policy)
	}
	return p.Name, p.Rules, 0, nil
}

func (s *Server) check(w http.ResponseWriter, r *http.Request) {
	var req CheckRequest
	if !decode(w, r, &req) {
		return
	}
	ns, rules, status, err := s.resolve(req.Policy, req.Rules)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	if req.Cost == 0 {
		req.Cost = 1
	}
	res, err := s.Store.Allow(r.Context(), limiter.Request{
		Namespace: ns, Key: req.Key, Rules: rules, Cost: req.Cost, Consume: !req.Peek,
	})
	if errors.Is(err, limiter.ErrValidation) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		slog.Error("store error", "err", err)
		if s.FailOpen {
			writeJSON(w, http.StatusOK, CheckResponse{Result: limiter.Result{Allowed: true}, Policy: ns, Degraded: true, Error: "store unavailable"})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, CheckResponse{Policy: ns, Degraded: true, Error: "store unavailable"})
		return
	}
	if p, ok := res.Primary(); ok {
		w.Header().Set("X-RateLimit-Limit", fmt.Sprint(p.Limit))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprint(p.Remaining))
	}
	code := http.StatusOK
	if !res.Allowed {
		code = http.StatusTooManyRequests
		w.Header().Set("Retry-After", fmt.Sprint((res.RetryAfterMs+999)/1000))
	}
	writeJSON(w, code, CheckResponse{Result: res, Policy: ns})
}

func (s *Server) reset(w http.ResponseWriter, r *http.Request) {
	var req CheckRequest
	if !decode(w, r, &req) {
		return
	}
	ns, rules, status, err := s.resolve(req.Policy, req.Rules)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	if err := s.Store.Reset(r.Context(), limiter.Request{Namespace: ns, Key: req.Key, Rules: rules}); err != nil {
		code := http.StatusServiceUnavailable
		if errors.Is(err, limiter.ErrValidation) {
			code = http.StatusUnprocessableEntity
		}
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) list(w http.ResponseWriter, _ *http.Request) {
	out := make([]limiter.Policy, 0, len(s.Policies))
	for _, p := range s.Policies {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"policies": out})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json: " + err.Error()})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
