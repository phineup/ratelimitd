# ratelimitd — C++ SDK

C++17 client for [ratelimitd](https://github.com/phineup/ratelimitd). libcurl is used for HTTP (replaceable through the
`Transport` interface); JSON is built in, so libcurl is the only dependency. Available as a compiled library or header-only.

The xmake package lives in [phineup/xmake-repo](https://github.com/phineup/xmake-repo); this repository ships the CMake targets and the root `xmake.lua` used for development.

## Install

### xmake (recommended)

Add the Phineup package repository (always pass `master`, the stable snapshot; a release tag such as `v1.0.0` also works):

```lua
-- xmake.lua of your project
add_repositories("phineup https://github.com/phineup/xmake-repo.git master")
add_requires("ratelimitd")

target("app")
    set_kind("binary")
    set_languages("c++17")
    add_files("main.cpp")
    add_packages("ratelimitd")
```

Package configs: `add_requires("ratelimitd", {configs = {shared = true}})` for a shared library, `{configs = {headeronly = true}}` for header-only (libcurl is still linked).
Then `xmake` builds the app; libcurl is resolved by xmake automatically.

### CMake — FetchContent

```cmake
include(FetchContent)
FetchContent_Declare(ratelimitd
  GIT_REPOSITORY https://github.com/phineup/ratelimitd.git
  GIT_TAG        v1.1.0)
FetchContent_MakeAvailable(ratelimitd)

target_link_libraries(app PRIVATE ratelimitd::cpp)              # compiled library
# or
target_link_libraries(app PRIVATE ratelimitd::cpp_header_only)  # header-only (defines RATELIMITD_HEADER_ONLY, still needs libcurl)
```

### CMake — install, then `find_package`

```bash
git clone https://github.com/phineup/ratelimitd.git && cd ratelimitd
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build
cmake --install build --prefix /usr/local     # headers + library + CMake package files
```

```cmake
find_package(ratelimitd CONFIG REQUIRED)
target_link_libraries(app PRIVATE ratelimitd::cpp)
```

### Copy the sources

Only `sdk/cpp/include/ratelimitd/**` is needed for header-only use. Define `RATELIMITD_HEADER_ONLY` and link libcurl.

## Usage

```cpp
#include <ratelimitd/ratelimitd.hpp>
#include <iostream>
#include <thread>

int main() {
    ratelimitd::ClientOptions opt;
    opt.token = "secret";                          // only when the server has auth enabled
    opt.timeout = std::chrono::milliseconds(2000);
    ratelimitd::Client rl("http://localhost:8080", opt);

    // Named policy defined in the server config.
    auto res = rl.check(ratelimitd::CheckRequest::for_policy("user-42", "login"));
    if (!res.allowed) {
        // 429 is a normal result, not an exception
        std::cout << "limited by " << res.limited_by << ", retry in " << res.retry_after().count() << " ms\n";
    }

    // Inline rules, no server config needed.
    rl.check(ratelimitd::CheckRequest::for_rules(
        "1.2.3.4", {ratelimitd::Rule::make("sliding_window", 10, "1m")}));

    // Cost and peek (look without consuming).
    auto req = ratelimitd::CheckRequest::for_policy("u", "login");
    req.cost = 3;
    req.peek = true;
    rl.check(req);

    // Shorthand, reset, introspection.
    bool ok = rl.allow("login", "user-42");
    rl.reset(ratelimitd::CheckRequest::for_policy("user-42", "login"));
    auto policies = rl.policies();
    rl.healthy();
    (void)ok; (void)policies;
}
```

## API

| method | description |
|---|---|
| `Client(base_url, ClientOptions)` | options: `token`, `timeout`, `transport` (custom `Transport`) |
| `check(CheckRequest)` | evaluate and (unless `peek`) consume |
| `allow(policy, key)` | shorthand returning `bool` |
| `reset(CheckRequest)` | clear counters |
| `policies()` | `std::vector<Policy>` |
| `healthy()` | returns normally when healthy |

`CheckResult`: `allowed`, `consumed`, `remaining`, `retry_after_ms`, `retry_after()` (`std::chrono::milliseconds`), `limited_by`,
`rules`, `policy`, `degraded`, `error`. Other non-2xx responses throw `ApiError` (`status()`); network failures throw `TransportError`.
The client is thread-safe.

## Build and test

```bash
cmake -S . -B build && cmake --build build && ctest --test-dir build --output-on-failure   # from the repo root
xmake && xmake test                                                                         # xmake
RATELIMITD_URL=http://127.0.0.1:18080 ctest --test-dir build                               # + real-server test
```
