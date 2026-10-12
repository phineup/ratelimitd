# ratelimitd (Ruby)

Standard-library-only Ruby (>= 3.0) client for [ratelimitd](https://github.com/phineup/ratelimitd).

```ruby
require "ratelimitd"

rl = Ratelimitd::Client.new("http://localhost:8080", token: "secret")
res = rl.check(key: "user-42", policy: "login")
puts "retry in #{res.retry_after}s" unless res.allowed   # 429 is a result, not an exception
```

Methods: `check`, `allow?`, `reset`, `policies`, `healthy`. Other non-2xx responses raise `Ratelimitd::ApiError` (`#status`).

```
rake test
RATELIMITD_URL=http://127.0.0.1:18080 rake test
```
