# ratelimitd (Go)

Standard-library-only Go client for [ratelimitd](https://github.com/phineup/ratelimitd).

```go
import ratelimitd "github.com/phineup/ratelimitd/sdk/go"

rl := ratelimitd.New("http://localhost:8080", ratelimitd.WithToken("secret"))
res, err := rl.Check(ctx, ratelimitd.CheckRequest{Key: "user-42", Policy: "login"})
if err == nil && !res.Allowed {
    time.Sleep(res.RetryAfter()) // 429 is a normal result, not an error
}
```

Methods: `Check`, `Allow`, `Reset`, `Policies`, `Healthy`. Other non-2xx responses are `*APIError` (`IsStatus(err, 404)`).

```
go test -race ./...
RATELIMITD_URL=http://127.0.0.1:18080 go test -race ./...
```
