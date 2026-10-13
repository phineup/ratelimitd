# ratelimitd client SDKs

Official client libraries for [ratelimitd](https://github.com/phineup/ratelimitd), the single-binary rate limiting sidecar.
All SDKs live in this monorepo, talk to the same HTTP API and expose the same operations, so you can mix languages freely.

| language | directory | package | install source | HTTP / JSON |
|---|---|---|---|---|
| Go | [`go`](go) | `github.com/phineup/ratelimitd/sdk/go` | Go modules (`sdk/go/vX.Y.Z` tags) | stdlib |
| TypeScript / JavaScript | [`typescript`](typescript) | `@phineup/ratelimitd` | GitHub Packages (npm) | global `fetch` |
| Python | [`python`](python) | `ratelimitd` | `pip install git+https://…#subdirectory=sdk/python` | stdlib |
| Java | [`java`](java) | `com.phineup:ratelimitd` | GitHub Packages (Maven) or `mvn install` | `java.net.http` + Jackson |
| C# / .NET | [`csharp`](csharp) | `Phineup.Ratelimitd` | GitHub Packages (NuGet) or project reference | `HttpClient` + `System.Text.Json` |
| Rust | [`rust`](rust) | `ratelimitd` | cargo git dependency | `ureq` + `serde` |
| C++ | [`cpp`](cpp) | xmake `ratelimitd`, CMake `ratelimitd::cpp` | xmake (`phineup/xmake-repo`), FetchContent, `find_package` | libcurl, built-in JSON |
| PHP | [`php`](php) | `phineup/ratelimitd` | Composer VCS repository | ext-curl |
| Ruby | [`ruby`](ruby) | `ratelimitd` | Bundler git source (`glob:`) | `net/http` |

Each directory has its own README with installation, a full usage example and the API reference.

## Common behaviour

Operations: `check` (with a `policy` name **or** inline `rules`, plus `cost` and `peek`), `allow`, `reset`, `policies`, `healthy`.

- A denied request (HTTP 429) is a **normal result** with `allowed == false`, `limited_by` and `retry_after_ms`, not an error.
- Any other non-2xx response is an error carrying the HTTP status (`400` malformed, `401` token, `404` unknown policy, `422` invalid input, `503` store down with `on_error: deny`).
- `degraded: true` on a result means Redis was unavailable and the server failed open (`on_error: allow`).
- Pass the bearer token when the server runs with `RATELIMITD_AUTH_TOKENS`.
- Clients have no state besides configuration and are safe to share between threads/goroutines.

## Build systems: CMake and xmake

This repository is a **CMake project** (root `CMakeLists.txt`) and has a root `xmake.lua` for developing the C++ and Rust SDKs:

```bash
# CMake (root CMakeLists.txt): C++ SDK, optionally Rust through cargo
cmake -S . -B build -DRATELIMITD_BUILD_RUST=ON
cmake --build build && ctest --test-dir build --output-on-failure

# xmake (root xmake.lua)
xmake f --rust=y && xmake build && xmake test
```

Use the C++ SDK from your own xmake project through [phineup/xmake-repo](https://github.com/phineup/xmake-repo):

```lua
add_repositories("phineup https://github.com/phineup/xmake-repo.git master")
add_requires("ratelimitd")
target("app")
    set_kind("binary")
    add_files("main.cpp")
    add_packages("ratelimitd")
```

Rust is a regular cargo crate, so use it with a cargo git dependency.

## Versioning and publishing

Git tags and the container image use the `vX.Y.Z` form (`v1.1.0`, `ghcr.io/phineup/ratelimitd:v1.1.0`). Language
package registries normalize or reject a leading `v`, so packages are published as plain `X.Y.Z` built from the same tag.
`.github/workflows/sdk-publish.yml` runs on version tags only and, after the integration tests pass:

| ecosystem | where it goes |
|---|---|
| TypeScript / JavaScript | GitHub Packages npm (`npm.pkg.github.com`), never npmjs |
| Java | GitHub Packages Maven (`maven.pkg.github.com/phineup/ratelimitd`) |
| C# | GitHub Packages NuGet (`nuget.pkg.github.com/phineup`) |
| Go | pushes the `sdk/go/vX.Y.Z` tag the Go module system needs for subdirectory modules |
| Python, Rust, Ruby, PHP, C++ | installed from the Git tag (no registry publish) |

Reading GitHub Packages (npm, Maven, NuGet) requires a token with `read:packages`; each SDK README shows the exact configuration.

## Tests

Every SDK has unit tests that need no server (in-process fake or mock HTTP server) plus integration tests that run against a
real `ratelimitd` and are skipped unless `RATELIMITD_URL` is set:

```bash
go run ./cmd/ratelimitd -config sdk/test-config.json     # listens on 127.0.0.1:18080
export RATELIMITD_URL=http://127.0.0.1:18080
```

CI runs all of this in `.github/workflows/sdk.yml`.
