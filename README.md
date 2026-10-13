# ratelimitd

A single-binary Go sidecar rate limiting service. Zero dependencies (stdlib only).

- **Four algorithms**: fixed window (clock or first-request anchored), sliding window, sliding log, token bucket.
- **Multi-rule policies**: a request is counted only if every rule allows it, so a denial never burns quota.
- **Memory or Redis store**: single instance in-process, or shared across instances with one atomic Lua call per decision.
- **Language-agnostic**: plain HTTP/JSON API plus official SDKs for Go, TypeScript/JavaScript, Python, Java, C#, Rust, C++, PHP and Ruby.
- **Fails open by default**: if Redis is down, requests are allowed and flagged `degraded: true` (configurable with `on_error`).

## Contents

[Run](#run) · [Algorithms](#algorithms-algorithm) · [API](#api) · [Client SDKs](#client-sdks) · [Test](#test) · [Notes](#notes) · [Contributing](#contributing)

## Run

The server listens on `:8080`. Check it with `curl localhost:8080/healthz`.

```bash
docker run -p 8080:8080 ghcr.io/phineup/ratelimitd:latest   # or pin a release: ghcr.io/phineup/ratelimitd:v1.0.1
```

Prebuilt binaries (Linux and macOS, amd64 and arm64) are attached to every
[GitHub release](https://github.com/phineup/ratelimitd/releases) as `ratelimitd_vX.Y.Z_<os>_<arch>.tar.gz`
with a `checksums.txt`. Release tags, container image tags and `ratelimitd -version` all use the same
`vX.Y.Z` form.

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

## Client SDKs

Official clients live in [`sdk/`](sdk) and share the same operations (`check`, `allow`, `reset`, `policies`, `healthy`).
Each has its own README with installation and full examples; see the [SDK overview](sdk/README.md).

| language | install | docs |
|---|---|---|
| Go | `go get github.com/phineup/ratelimitd/sdk/go` | [sdk/go](sdk/go) |
| TypeScript / JavaScript | `npm i @phineup/ratelimitd` (GitHub Packages) | [sdk/typescript](sdk/typescript) |
| Python | `pip install "git+https://github.com/phineup/ratelimitd.git#subdirectory=sdk/python"` | [sdk/python](sdk/python) |
| Java | Maven `com.phineup:ratelimitd` (GitHub Packages) | [sdk/java](sdk/java) |
| C# / .NET | `dotnet add package Phineup.Ratelimitd` (GitHub Packages) | [sdk/csharp](sdk/csharp) |
| Rust | `ratelimitd = { git = "https://github.com/phineup/ratelimitd" }` | [sdk/rust](sdk/rust) |
| C++ | xmake (`phineup/xmake-repo`) or CMake `ratelimitd::cpp` | [sdk/cpp](sdk/cpp) |
| PHP | Composer VCS repository `phineup/ratelimitd` | [sdk/php](sdk/php) |
| Ruby | Bundler git source with `glob: "sdk/ruby/*.gemspec"` | [sdk/ruby](sdk/ruby) |

### xmake package

The C++ SDK is available as an xmake package from [phineup/xmake-repo](https://github.com/phineup/xmake-repo) (always pass `master`, the stable snapshot):

```lua
add_repositories("phineup https://github.com/phineup/xmake-repo.git master")
add_requires("ratelimitd")

target("app")
    set_kind("binary")
    add_files("main.cpp")
    add_packages("ratelimitd")
```

CMake works as well (`FetchContent` or `find_package(ratelimitd)`), see [sdk/cpp](sdk/cpp/README.md).

### Versioning

Release tags, container image tags and `ratelimitd -version` all use the `vX.Y.Z` form. Language packages are published
as plain `X.Y.Z` from the same tag, because package registries normalize or reject a leading `v`. Details in [sdk/README.md](sdk/README.md#versioning-and-publishing).

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
