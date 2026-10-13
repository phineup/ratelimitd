# ratelimitd — Ruby SDK

Standard-library-only (`net/http`) Ruby client for [ratelimitd](https://github.com/phineup/ratelimitd). Ruby >= 3.0.

## Install

### GitHub Packages (RubyGems)

Releases are published to GitHub Packages (not rubygems.org). Reading needs a personal access token with `read:packages`:

```bash
bundle config set --global https://rubygems.pkg.github.com/phineup YOUR_GITHUB_USERNAME:YOUR_TOKEN
```

```ruby
# Gemfile
source "https://rubygems.pkg.github.com/phineup" do
  gem "ratelimitd", "~> 1.1"
end
```

### Git source (no token)

The gem lives in a subdirectory, so pass `glob:`:

```ruby
# Gemfile
gem "ratelimitd",
    git: "https://github.com/phineup/ratelimitd.git",
    tag: "v1.1.0",
    glob: "sdk/ruby/*.gemspec"
```

```bash
bundle install
```

Use `branch: "master"` instead of `tag:` to track the latest. From a local checkout: `gem "ratelimitd", path: "../ratelimitd/sdk/ruby"`.
Or build the gem yourself: `cd sdk/ruby && gem build ratelimitd.gemspec && gem install ./ratelimitd-0.1.0.gem`.

## Usage

```ruby
require "ratelimitd"

rl = Ratelimitd::Client.new("http://localhost:8080", token: "secret", timeout: 2.0) # token only if auth is enabled

# Named policy defined in the server config. Requests are plain hashes.
res = rl.check(key: "user-42", policy: "login")
unless res.allowed
  # 429 is a normal result, not an exception
  puts "limited by #{res.limited_by}, retry in #{res.retry_after}s"
end

# Inline rules, no server config needed.
rl.check(key: "1.2.3.4", rules: [{ algorithm: "sliding_window", limit: 10, window: "1m" }])

# Cost and peek (look without consuming).
rl.check(key: "u", policy: "login", cost: 3, peek: true)

# Shorthand, reset, introspection.
ok = rl.allow?("login", "user-42")
rl.reset(key: "user-42", policy: "login")
rl.policies
rl.healthy
```

### Rack middleware sketch

```ruby
class RateLimit
  def initialize(app, client:, policy:) = (@app, @rl, @policy = app, client, policy)

  def call(env)
    res = @rl.check(key: Rack::Request.new(env).ip, policy: @policy)
    return [429, { "Retry-After" => res.retry_after.ceil.to_s }, ["rate limited"]] unless res.allowed
    @app.call(env)
  rescue Ratelimitd::TransportError
    @app.call(env) # fail open
  end
end
```

## API

| method | description |
|---|---|
| `Client.new(base_url, token: nil, timeout: 10.0, transport: nil)` | thread-safe; `transport` is an optional callable |
| `check(request_hash)` | keys: `key`, `policy`, `rules`, `cost`, `peek` |
| `allow?(policy, key)` | shorthand returning a boolean |
| `reset(request_hash)` | clear counters |
| `policies` | configured policies |
| `healthy` | raises when unhealthy |

`CheckResult` (Struct): `allowed`, `consumed`, `remaining`, `retry_after_ms`, `retry_after` (seconds), `limited_by`, `rules`, `policy`,
`degraded`, `error`. Non-2xx responses other than 429 raise `Ratelimitd::ApiError` (`#status`); network failures raise `Ratelimitd::TransportError`.

## Test

```bash
rake test
RATELIMITD_URL=http://127.0.0.1:18080 rake test
```
