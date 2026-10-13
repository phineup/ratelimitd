# ratelimitd — Python SDK

Standard-library-only Python client for [ratelimitd](https://github.com/phineup/ratelimitd). Python >= 3.9, fully typed (`py.typed`).

## Install

Not on PyPI yet; install straight from the GitHub repository (the package is in the `sdk/python` subdirectory):

```bash
pip install "git+https://github.com/phineup/ratelimitd.git@v1.1.0#subdirectory=sdk/python"
# latest development state:
pip install "git+https://github.com/phineup/ratelimitd.git@master#subdirectory=sdk/python"
```

In `requirements.txt` / `pyproject.toml`:

```
ratelimitd @ git+https://github.com/phineup/ratelimitd.git@v1.1.0#subdirectory=sdk/python
```

From a local checkout: `pip install ./sdk/python` (or `pip install -e ./sdk/python` for development).

## Usage

```python
from ratelimitd import Client, Rule

rl = Client("http://localhost:8080", token="secret", timeout=2.0)   # token only if auth is enabled

# Named policy defined in the server config.
res = rl.check("user-42", policy="login")
if not res.allowed:
    # 429 is a result, not an exception
    print(f"limited by {res.limited_by}, retry in {res.retry_after_seconds}s")

# Inline rules, no server config needed.
rl.check("1.2.3.4", rules=[Rule(algorithm="sliding_window", limit=10, window="1m")])
rl.check("user-42", rules=[Rule(algorithm="token_bucket", limit=30, window="1m", burst=5)])

# Cost and peek (look without consuming).
rl.check("u", policy="login", cost=3, peek=True)

# Shorthand, reset, introspection.
ok = rl.allow("login", "user-42")
rl.reset("user-42", policy="login")
policies = rl.policies()
rl.healthy()
```

### Flask sketch

```python
@app.before_request
def limit():
    try:
        r = rl.check(request.remote_addr, policy="api_default")
    except Exception:
        return None                      # fail open
    if not r.allowed:
        return ("rate limited", 429, {"Retry-After": str(int(r.retry_after_seconds) + 1)})
```

## API

| method | description |
|---|---|
| `Client(base_url, token=None, timeout=10.0)` | create a client (thread-safe, no persistent state) |
| `check(key, *, policy=None, rules=None, cost=None, peek=False)` | returns `CheckResult` |
| `allow(policy, key)` | shorthand returning a bool |
| `reset(key, *, policy=None, rules=None)` | clear counters |
| `policies()` | list of `Policy` |
| `healthy()` | returns `None`, raises when unhealthy |

`CheckResult`: `allowed`, `consumed`, `remaining`, `retry_after_ms`, `retry_after_seconds`, `limited_by`, `rules`, `policy`,
`degraded`, `error`. Non-2xx responses other than 429 raise `APIError` (`.status`).

## Test

```bash
python -m unittest discover -s tests -t .
RATELIMITD_URL=http://127.0.0.1:18080 python -m unittest discover -s tests -t .
```
