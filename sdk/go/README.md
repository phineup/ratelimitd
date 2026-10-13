# ratelimitd — Go SDK

Standard-library-only Go client for [ratelimitd](https://github.com/phineup/ratelimitd). No third-party dependencies.

- Module: `github.com/phineup/ratelimitd/sdk/go`
- Requires: Go 1.24+ (see `go.mod`)
- Server: a running `ratelimitd` (see the [main README](../../README.md#run))

## Install

```bash
go get github.com/phineup/ratelimitd/sdk/go@latest
```

The module lives in the `sdk/go` subdirectory, so Go resolves versions from tags named `sdk/go/vX.Y.Z`
(the maintainer pushes `sdk/go/vX.Y.Z` together with every `vX.Y.Z` release). Until a tag for the version you want
exists you can use a branch or commit:

```bash
go get github.com/phineup/ratelimitd/sdk/go@master        # or @development, or @<commit-sha>
```

For local development against a checkout:

```
// go.mod
replace github.com/phineup/ratelimitd/sdk/go => ../ratelimitd/sdk/go
```

## Usage

```go
package main

import (
	"context"
	"fmt"
	"time"

	ratelimitd "github.com/phineup/ratelimitd/sdk/go"
)

func main() {
	rl := ratelimitd.New("http://localhost:8080",
		ratelimitd.WithToken("secret"),            // only when the server has auth enabled
		// ratelimitd.WithHTTPClient(&http.Client{Timeout: 2 * time.Second}),
	)
	ctx := context.Background()

	// Named policy defined in the server config.
	res, err := rl.Check(ctx, ratelimitd.CheckRequest{Key: "user-42", Policy: "login"})
	if err != nil {
		panic(err) // transport problem or non-2xx other than 429
	}
	if !res.Allowed {
		// 429 is a normal result, not an error.
		fmt.Printf("limited by %s, retry in %s\n", res.LimitedBy, res.RetryAfter())
		time.Sleep(res.RetryAfter())
	}

	// Inline rules, no server config needed.
	res, _ = rl.Check(ctx, ratelimitd.CheckRequest{
		Key: "1.2.3.4",
		Rules: []ratelimitd.Rule{
			{Algorithm: "sliding_window", Limit: 10, Window: "1m"},
		},
	})

	// Cost and peek (look without consuming).
	res, _ = rl.Check(ctx, ratelimitd.CheckRequest{Key: "u", Policy: "login", Cost: 3, Peek: true})

	// Shorthand and reset.
	ok, _ := rl.Allow(ctx, "login", "user-42")
	_ = rl.Reset(ctx, ratelimitd.CheckRequest{Key: "user-42", Policy: "login"})
	_ = ok
}
```

### HTTP middleware sketch

```go
func limit(rl *ratelimitd.Client, policy string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res, err := rl.Check(r.Context(), ratelimitd.CheckRequest{Key: r.RemoteAddr, Policy: policy})
		if err == nil && !res.Allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(res.RetryAfter().Seconds())+1))
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r) // fail open on transport errors
	})
}
```

## API

| method | description |
|---|---|
| `New(baseURL, ...Option)` | create a client; options: `WithToken`, `WithHTTPClient` |
| `Check(ctx, CheckRequest) (Result, error)` | evaluate and (unless `Peek`) consume |
| `Allow(ctx, policy, key) (bool, error)` | shorthand for cost-1 policy check |
| `Reset(ctx, CheckRequest) error` | clear counters for a key |
| `Policies(ctx) ([]Policy, error)` | list configured policies |
| `Healthy(ctx) error` | `GET /healthz` |

`Result` carries `Allowed`, `Consumed`, `Remaining`, `RetryAfterMs`, `LimitedBy`, `Rules` (per-rule detail),
`Policy`, `Degraded` (server failed open because Redis is down) and `Error`. `Result.RetryAfter()` returns a `time.Duration`.

Errors: non-2xx responses other than 429 are `*ratelimitd.APIError`; use `ratelimitd.IsStatus(err, 404)` to test the status.

## Test

```bash
go test -race ./...
# plus the real-server tests (start the server first, see ../README.md)
RATELIMITD_URL=http://127.0.0.1:18080 go test -race ./...
```
