# ratelimitd (Rust)

Blocking Rust client for [ratelimitd](https://github.com/phineup/ratelimitd) (`serde` + `ureq`). Disable the default `tls` feature for plain-http sidecar use.

```rust
use ratelimitd::{CheckRequest, Client};

let rl = Client::new("http://localhost:8080").with_token("secret");
let res = rl.check(&CheckRequest::policy("user-42", "login"))?;
if !res.allowed {
    // 429 is a normal result
    println!("retry in {:?}", res.retry_after());
}
```

Methods: `check`, `allow`, `reset`, `policies`, `healthy`. Other non-2xx responses are `Error::Api { status, message }`.

```
cargo test
RATELIMITD_URL=http://127.0.0.1:18080 cargo test
```

It is also part of the repository's CMake and xmake builds; see the root `CMakeLists.txt` / `xmake.lua`.
