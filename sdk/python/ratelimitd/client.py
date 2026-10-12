from __future__ import annotations

import json
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional


class APIError(Exception):
    """Non-2xx response other than 429 (a denial is a normal CheckResult)."""

    def __init__(self, status: int, message: str) -> None:
        super().__init__(f"ratelimitd: HTTP {status}: {message}")
        self.status = status
        self.message = message


@dataclass
class Rule:
    """An inline rule. ``window`` uses the server syntax: "30s", "5h", "1d12h"."""

    limit: int
    window: str
    algorithm: Optional[str] = None
    name: Optional[str] = None
    burst: Optional[int] = None
    anchor: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        d: Dict[str, Any] = {"limit": self.limit, "window": self.window}
        for k in ("name", "algorithm", "burst", "anchor"):
            v = getattr(self, k)
            if v is not None:
                d[k] = v
        return d

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Rule":
        return cls(
            limit=d["limit"],
            window=d["window"],
            algorithm=d.get("algorithm"),
            name=d.get("name"),
            burst=d.get("burst"),
            anchor=d.get("anchor"),
        )


@dataclass
class RuleResult:
    name: str
    algorithm: str
    allowed: bool
    limit: int
    remaining: int
    retry_after_ms: int
    reset_after_ms: int


@dataclass
class CheckResult:
    allowed: bool
    consumed: bool = False
    remaining: int = 0
    retry_after_ms: int = 0
    limited_by: Optional[str] = None
    rules: List[RuleResult] = field(default_factory=list)
    policy: str = ""
    degraded: bool = False
    error: Optional[str] = None

    @property
    def retry_after_seconds(self) -> float:
        return self.retry_after_ms / 1000.0

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "CheckResult":
        rules = [
            RuleResult(
                name=r.get("name", ""),
                algorithm=r.get("algorithm", ""),
                allowed=r.get("allowed", False),
                limit=r.get("limit", 0),
                remaining=r.get("remaining", 0),
                retry_after_ms=r.get("retry_after_ms", 0),
                reset_after_ms=r.get("reset_after_ms", 0),
            )
            for r in d.get("rules") or []
        ]
        return cls(
            allowed=d.get("allowed", False),
            consumed=d.get("consumed", False),
            remaining=d.get("remaining", 0),
            retry_after_ms=d.get("retry_after_ms", 0),
            limited_by=d.get("limited_by"),
            rules=rules,
            policy=d.get("policy", ""),
            degraded=d.get("degraded", False),
            error=d.get("error"),
        )


@dataclass
class Policy:
    name: str
    rules: List[Rule]
    description: str = ""
    dynamic: bool = False


class Client:
    """Thread-safe, synchronous client."""

    def __init__(self, base_url: str, token: Optional[str] = None, timeout: float = 10.0) -> None:
        self.base_url = base_url.rstrip("/")
        self.token = token
        self.timeout = timeout

    def check(
        self,
        key: str,
        *,
        policy: Optional[str] = None,
        rules: Optional[List[Rule]] = None,
        cost: Optional[int] = None,
        peek: bool = False,
    ) -> CheckResult:
        """Evaluate (and by default consume) a request. Set ``policy`` or ``rules``."""
        status, body = self._request("POST", "/v1/check", _check_body(key, policy, rules, cost, peek))
        if status not in (200, 429):
            raise _error(status, body)
        return CheckResult.from_dict(json.loads(body))

    def allow(self, policy: str, key: str) -> bool:
        return self.check(key, policy=policy).allowed

    def reset(
        self,
        key: str,
        *,
        policy: Optional[str] = None,
        rules: Optional[List[Rule]] = None,
    ) -> None:
        status, body = self._request("POST", "/v1/reset", _check_body(key, policy, rules, None, False))
        if status not in (200, 204):
            raise _error(status, body)

    def policies(self) -> List[Policy]:
        status, body = self._request("GET", "/v1/policies", None)
        if status != 200:
            raise _error(status, body)
        return [
            Policy(
                name=p["name"],
                description=p.get("description", ""),
                rules=[Rule.from_dict(r) for r in p.get("rules") or []],
                dynamic=p.get("dynamic", False),
            )
            for p in json.loads(body).get("policies") or []
        ]

    def healthy(self) -> None:
        """Returns None when healthy, raises APIError otherwise."""
        status, body = self._request("GET", "/healthz", None)
        if status != 200:
            raise _error(status, body)

    def _request(self, method: str, path: str, payload: Optional[dict]) -> "tuple[int, bytes]":
        data = None
        headers = {}
        if payload is not None:
            data = json.dumps(payload).encode()
            headers["Content-Type"] = "application/json"
        if self.token:
            headers["Authorization"] = f"Bearer {self.token}"
        req = urllib.request.Request(self.base_url + path, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                return resp.status, resp.read()
        except urllib.error.HTTPError as e:
            try:
                return e.code, e.read()
            finally:
                e.close()


def _check_body(
    key: str,
    policy: Optional[str],
    rules: Optional[List[Rule]],
    cost: Optional[int],
    peek: bool,
) -> Dict[str, Any]:
    body: Dict[str, Any] = {"key": key}
    if policy:
        body["policy"] = policy
    if rules:
        body["rules"] = [r.to_dict() for r in rules]
    if cost:
        body["cost"] = cost
    if peek:
        body["peek"] = True
    return body


def _error(status: int, body: bytes) -> APIError:
    text = body.decode("utf-8", "replace").strip()
    msg = text
    try:
        parsed = json.loads(text)
        if isinstance(parsed, dict) and parsed.get("error"):
            msg = parsed["error"]
    except ValueError:
        pass
    return APIError(status, msg or f"HTTP {status}")
