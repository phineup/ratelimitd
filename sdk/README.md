# ratelimitd client SDKs

| language | path | package | HTTP / JSON |
|---|---|---|---|
| Go | [`go`](go) | `github.com/phineup/ratelimitd/sdk/go` | stdlib |
| TypeScript / JavaScript | [`typescript`](typescript) | `@phineup/ratelimitd` (GitHub Packages) | global `fetch` |
| Python | [`python`](python) | `ratelimitd` | stdlib |
| Java | [`java`](java) | `com.phineup:ratelimitd` | `java.net.http` + Jackson |
| C# / .NET | [`csharp`](csharp) | `Phineup.Ratelimitd` | `HttpClient` + `System.Text.Json` |
| Rust | [`rust`](rust) | `ratelimitd` | `ureq` + `serde` |
| C++ | [`cpp`](cpp) | CMake `ratelimitd::cpp`, xmake `ratelimitd` | libcurl, built-in JSON |
| PHP | [`php`](php) | `phineup/ratelimitd` | ext-curl |
| Ruby | [`ruby`](ruby) | `ratelimitd` | `net/http` |

All SDKs expose the same operations: `check` (with `policy` or inline `rules`, `cost`, `peek`), `allow`, `reset`, `policies`, `healthy`.
A denied request (HTTP 429) is returned as a normal result with `allowed == false` and `retry_after_ms`; other non-2xx responses are errors.

## Tests

Every SDK has unit tests that need no server (in-process fake or mock HTTP server) plus integration tests that
run against a real `ratelimitd` and are skipped unless `RATELIMITD_URL` is set:

```bash
go run ./cmd/ratelimitd -config sdk/test-config.json     # listens on 127.0.0.1:18080
export RATELIMITD_URL=http://127.0.0.1:18080
```

CI runs all of this in `.github/workflows/sdk.yml`. Registry publishing is in `sdk-publish.yml` and
currently covers the npm package on GitHub Packages only; nothing is published to npmjs.
