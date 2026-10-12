package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ratelimitd/limiter"
	"ratelimitd/server"
)

type config struct {
	Listen string `json:"listen"`
	Store  struct {
		Type      string `json:"type"` // memory | redis
		RedisURL  string `json:"redis_url"`
		KeyPrefix string `json:"key_prefix"`
	} `json:"store"`
	OnError       string                    `json:"on_error"` // allow (default) | deny
	AuthTokens    []string                  `json:"auth_tokens"`
	DefaultPolicy string                    `json:"default_policy"`
	Policies      map[string]limiter.Policy `json:"policies"`
}

func main() {
	path := flag.String("config", os.Getenv("RATELIMITD_CONFIG"), "path to JSON config")
	health := flag.Bool("healthcheck", false, "probe /healthz on localhost and exit (for Docker)")
	flag.Parse()

	cfg := config{Listen: ":8080", OnError: "allow"}
	cfg.Store.Type = "memory"
	if *path != "" {
		b, err := os.ReadFile(*path)
		if err == nil {
			err = json.Unmarshal(b, &cfg)
		}
		if err != nil {
			fatal("config: %v", err)
		}
	}
	// Environment overrides.
	if v := os.Getenv("RATELIMITD_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("RATELIMITD_STORE"); v != "" {
		cfg.Store.Type = v
	}
	if v := os.Getenv("RATELIMITD_REDIS_URL"); v != "" {
		cfg.Store.RedisURL = v
	}
	if v := os.Getenv("RATELIMITD_AUTH_TOKENS"); v != "" {
		cfg.AuthTokens = strings.Split(v, ",")
	}

	if *health {
		addr := cfg.Listen
		if strings.HasPrefix(addr, ":") {
			addr = "127.0.0.1" + addr
		}
		c := http.Client{Timeout: 2 * time.Second}
		resp, err := c.Get("http://" + addr + "/healthz")
		if err != nil || resp.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}

	for name, p := range cfg.Policies {
		p.Name = name
		if err := p.Normalize(); err != nil {
			fatal("policy %q: %v", name, err)
		}
		cfg.Policies[name] = p
	}
	if cfg.DefaultPolicy != "" {
		if _, ok := cfg.Policies[cfg.DefaultPolicy]; !ok {
			fatal("default_policy %q not found", cfg.DefaultPolicy)
		}
	}

	var store limiter.Store
	switch cfg.Store.Type {
	case "memory":
		store = limiter.NewMemoryStore()
	case "redis":
		rs, err := limiter.NewRedisStore(cfg.Store.RedisURL, cfg.Store.KeyPrefix)
		if err != nil {
			fatal("redis: %v", err)
		}
		// Wait for Redis so the sidecar can start alongside it.
		var perr error
		for i := 0; i < 30; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			perr = rs.Ping(ctx)
			cancel()
			if perr == nil {
				break
			}
			time.Sleep(time.Second)
		}
		if perr != nil {
			slog.Warn("redis not reachable yet; continuing", "err", perr)
		}
		store = rs
	default:
		fatal("store.type must be memory or redis")
	}

	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: (&server.Server{
			Store: store, Policies: cfg.Policies, DefaultPolicy: cfg.DefaultPolicy,
			Tokens: cfg.AuthTokens, FailOpen: cfg.OnError != "deny",
		}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		slog.Info("listening", "addr", cfg.Listen, "store", cfg.Store.Type, "policies", len(cfg.Policies))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fatal("%v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	_ = store.Close()
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ratelimitd: "+format+"\n", a...)
	os.Exit(1)
}
