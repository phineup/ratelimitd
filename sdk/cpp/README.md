# ratelimitd (C++)

C++17 client for [ratelimitd](https://github.com/phineup/ratelimitd). libcurl is used for HTTP (replaceable through the `Transport` interface); JSON is built in.

```cpp
#include <ratelimitd/ratelimitd.hpp>

ratelimitd::ClientOptions opt;
opt.token = "secret";
ratelimitd::Client rl("http://localhost:8080", opt);

auto res = rl.check(ratelimitd::CheckRequest::for_policy("user-42", "login"));
if (!res.allowed) {
    // 429 is a normal result; res.retry_after() is std::chrono::milliseconds
}
```

Methods: `check`, `allow`, `reset`, `policies`, `healthy`. Other non-2xx responses throw `ApiError` (`status()`); network failures throw `TransportError`.

Build with the repository's root `CMakeLists.txt` (shared with the Rust SDK), `sdk/cpp/CMakeLists.txt` standalone, or xmake (`xmake.lua`; the `ratelimitd` package is in [phineup/xmake-repo](https://github.com/phineup/xmake-repo)). Tests: `ctest`; set `RATELIMITD_URL` for the real-server test.

```cmake
find_package(ratelimitd CONFIG REQUIRED)
target_link_libraries(app PRIVATE ratelimitd::cpp)              # compiled library
target_link_libraries(app PRIVATE ratelimitd::cpp_header_only)  # header-only (defines RATELIMITD_HEADER_ONLY, still needs libcurl)
```
