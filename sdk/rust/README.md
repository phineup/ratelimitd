# ratelimitd — Rust SDK

Blocking Rust client for [ratelimitd](https://github.com/phineup/ratelimitd) (`serde` + `ureq`). MSRV 1.74.

## Install

Not on crates.io yet; depend on it through git. Cargo finds the `ratelimitd` crate inside the repository by name:

```toml
[dependencies]
ratelimitd = { git = "https://github.com/phineup/ratelimitd", tag = "v1.1.0" }
```

or `cargo add ratelimitd --git https://github.com/phineup/ratelimitd --tag v1.1.0`. Use `branch = "master"` to track the latest.
From a local checkout: `ratelimitd = { path = "../ratelimitd/sdk/rust" }`.

The default `tls` feature enables `https://`. For a plain-http sidecar drop the TLS stack:

```toml
ratelimitd = { git = "https://github.com/phineup/ratelimitd", tag = "v1.1.0", default-features = false }
```

## Usage

```rust
use ratelimitd::{CheckRequest, Client, Rule};
use std::time::Duration;

fn main() -> ratelimitd::Result<()> {
    let rl = Client::new("http://localhost:8080")
        .with_token("secret")                       // only when the server has auth enabled
        .with_timeout(Duration::from_secs(2));

    // Named policy defined in the server config.
    let res = rl.check(&CheckRequest::policy("user-42", "login"))?;
    if !res.allowed {
        // 429 is a normal result, not an error
        println!("limited by {:?}, retry in {:?}", res.limited_by, res.retry_after());
    }

    // Inline rules, no server config needed.
    rl.check(&CheckRequest::rules("1.2.3.4", vec![Rule::new("sliding_window", 10, "1m")]))?;
    rl.check(&CheckRequest::rules("u", vec![Rule::new("token_bucket", 30, "1m").with_burst(5)]))?;

    // Cost and peek (look without consuming).
    rl.check(&CheckRequest::policy("u", "login").cost(3).peek())?;

    // Shorthand, reset, introspection.
    let ok = rl.allow("login", "user-42")?;
    rl.reset(&CheckRequest::policy("user-42", "login"))?;
    let policies = rl.policies()?;
    rl.healthy()?;
    let _ = (ok, policies);
    Ok(())
}
```

The client is blocking; from async code call it inside `tokio::task::spawn_blocking` (or similar).

## API

| method | description |
|---|---|
| `Client::new(url)`, `.with_token`, `.with_timeout` | builder-style construction |
| `check(&CheckRequest) -> Result<CheckResult>` | evaluate and (unless `peek`) consume |
| `allow(policy, key) -> Result<bool>` | shorthand |
| `reset(&CheckRequest) -> Result<()>` | clear counters |
| `policies() -> Result<Vec<Policy>>` | list configured policies |
| `healthy() -> Result<()>` | `/healthz` |

`CheckResult`: `allowed`, `consumed`, `remaining`, `retry_after_ms`, `limited_by`, `rules`, `policy`, `degraded`, `error`, and
`retry_after()` as a `Duration`. Non-2xx responses other than 429 are `Error::Api { status, message }` (`Error::status()`).

## Build and test

```bash
cargo test
RATELIMITD_URL=http://127.0.0.1:18080 cargo test
```

The crate is also part of the repository's CMake (`-DRATELIMITD_BUILD_RUST=ON`) and xmake (`xmake f --rust=y`) builds, which
simply drive `cargo`; see the [SDK overview](../README.md#build-systems-cmake-and-xmake).
