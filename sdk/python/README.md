# ratelimitd (Python)

Standard-library-only Python client for [ratelimitd](https://github.com/phineup/ratelimitd). Python >= 3.9.

```python
from ratelimitd import Client, Rule

rl = Client("http://localhost:8080", token="secret")
res = rl.check("user-42", policy="login")
if not res.allowed:
    print(f"retry in {res.retry_after_seconds}s")   # 429 is a result, not an exception

rl.check("1.2.3.4", rules=[Rule(algorithm="sliding_window", limit=10, window="1m")])
```

Methods: `check`, `allow`, `reset`, `policies`, `healthy`. Other non-2xx responses raise `APIError` (`.status`).

```
python -m unittest discover -s tests -t .
RATELIMITD_URL=http://127.0.0.1:18080 python -m unittest discover -s tests -t .
```
