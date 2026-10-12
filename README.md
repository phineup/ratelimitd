# ratelimitd

A single-binary Go sidecar rate limiting service. Zero dependencies (stdlib only).

## Run

```bash
docker run -p 8080:8080 ghcr.io/phineup/ratelimitd:latest
```

From source:

```bash
go run ./cmd/ratelimitd -config config.example.json      # in-memory, single instance
docker compose up --build                                # with Redis, multiple instances
```

Environment: `RATELIMITD_LISTEN`, `RATELIMITD_STORE` (memory|redis), `RATELIMITD_REDIS_URL`,
`RATELIMITD_AUTH_TOKENS` (comma-separated bearer token list, empty = no auth), `RATELIMITD_CONFIG`.

## Algorithms (`algorithm`)

| value | behavior |
|---|---|
| `fixed_window` | `anchor: clock` (default, aligned to minute/hour boundaries) or `anchor: first_request` (starts at the first request; Claude-style 5-hour session / weekly limit) |
| `sliding_window` | weighted two-window approximation, O(1), approximate |
| `sliding_log` | exact, O(limit) memory (max 100000) |
| `token_bucket` | refills at `limit`/`window` rate, `burst` capacity |

`window`: `30s`, `5h`, `7d`, `1w`, `1d12h`. A policy can contain multiple rules; a request is counted only if **all** of them allow it. If any rule denies, none of the counters are incremented.

## API

```bash
# with a policy
curl -s localhost:8080/v1/check -d '{"key":"user-42","policy":"claude_style"}'
# with inline rules (no config needed)
curl -s localhost:8080/v1/check -d '{"key":"1.2.3.4","rules":[{"algorithm":"sliding_window","limit":10,"window":"1m"}]}'
# cost, peek (look without consuming), reset
curl -s localhost:8080/v1/check -d '{"key":"u","policy":"login","cost":1,"peek":true}'
curl -s localhost:8080/v1/reset -d '{"key":"u","policy":"login"}'
curl -s localhost:8080/v1/policies
```

Allowed: `200`. Limit exceeded: `429` + `Retry-After`; the body contains `allowed`, `limited_by`, `retry_after_ms`, and each rule's `remaining` / `reset_after_ms`.
Errors: `400` malformed JSON, `401` token, `404` unknown policy, `422` invalid input, `503` store unavailable (when `on_error: deny`).
With `on_error: allow` (default), if Redis goes down requests are let through with `degraded: true`.

When auth is enabled: `Authorization: Bearer <token>`. `/healthz` is always open.

## Test

```bash
go test -race ./...
```

The Redis tests temporarily start the `redis-server` found on your PATH (skipped if absent) and verify that memory and Redis results are identical.

## Notes

- Redis: single endpoint (`redis://` / `rediss://`), Redis >= 5. Every decision is a single atomic Lua call. Time comes from Redis `TIME`.
- Policies are read from the config file only; restart after changing them.

## Contributing

Contributions are welcome and go to the `development` branch (`master` holds stable releases). Please read [CONTRIBUTING.md](CONTRIBUTING.md) first; all
commits must be signed off under the [Developer Certificate of Origin](https://developercertificate.org/).
Report security issues privately as described in [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
