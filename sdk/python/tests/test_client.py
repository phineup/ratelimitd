import json
import os
import threading
import time
import unittest
import uuid
from http.server import BaseHTTPRequestHandler, HTTPServer

from ratelimitd import APIError, Client, Rule


class _Server:
    """Tiny in-process HTTP server; ``responder(method, path, body)`` -> (status, payload|None)."""

    def __init__(self, responder):
        outer = self
        self.requests = []

        class H(BaseHTTPRequestHandler):
            def _handle(self):
                n = int(self.headers.get("Content-Length") or 0)
                raw = self.rfile.read(n) if n else b""
                outer.requests.append(
                    {
                        "method": self.command,
                        "path": self.path,
                        "auth": self.headers.get("Authorization"),
                        "ct": self.headers.get("Content-Type"),
                        "body": json.loads(raw) if raw else None,
                    }
                )
                status, payload = responder(self.command, self.path, raw)
                self.send_response(status)
                if payload is None:
                    self.end_headers()
                    return
                data = payload if isinstance(payload, bytes) else json.dumps(payload).encode()
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            do_GET = do_POST = _handle

            def log_message(self, *a):
                pass

        self.httpd = HTTPServer(("127.0.0.1", 0), H)
        self.url = f"http://127.0.0.1:{self.httpd.server_address[1]}"
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()

    def close(self):
        self.httpd.shutdown()
        self.httpd.server_close()


class ClientTest(unittest.TestCase):
    def serve(self, responder):
        s = _Server(responder)
        self.addCleanup(s.close)
        return s

    def test_check_allowed_encodes_request(self):
        s = self.serve(lambda m, p, b: (200, {"allowed": True, "consumed": True, "remaining": 4, "policy": "login", "rules": []}))
        c = Client(s.url + "/", token="tok")
        r = c.check("u", policy="login", cost=2, peek=True)
        self.assertTrue(r.allowed)
        self.assertEqual(r.remaining, 4)
        req = s.requests[0]
        self.assertEqual(req["path"], "/v1/check")
        self.assertEqual(req["auth"], "Bearer tok")
        self.assertEqual(req["ct"], "application/json")
        self.assertEqual(req["body"], {"key": "u", "policy": "login", "cost": 2, "peek": True})

    def test_denied_is_a_result_not_an_error(self):
        s = self.serve(
            lambda m, p, b: (429, {"allowed": False, "retry_after_ms": 1500, "limited_by": "r", "policy": "p", "rules": [{"name": "r", "algorithm": "fixed_window", "allowed": False, "limit": 1, "remaining": 0, "retry_after_ms": 1500, "reset_after_ms": 1500}]})
        )
        c = Client(s.url)
        r = c.check("u", policy="p")
        self.assertFalse(r.allowed)
        self.assertEqual(r.retry_after_ms, 1500)
        self.assertEqual(r.retry_after_seconds, 1.5)
        self.assertEqual(r.limited_by, "r")
        self.assertEqual(r.rules[0].name, "r")
        self.assertFalse(c.allow("p", "u"))

    def test_inline_rules_serialization_omits_unset_fields(self):
        s = self.serve(lambda m, p, b: (200, {"allowed": True, "rules": [], "policy": "inline"}))
        Client(s.url).check("1.2.3.4", rules=[Rule(limit=10, window="1m", algorithm="token_bucket", burst=20)])
        body = s.requests[0]["body"]
        self.assertEqual(body["rules"], [{"limit": 10, "window": "1m", "algorithm": "token_bucket", "burst": 20}])
        self.assertNotIn("policy", body)
        self.assertNotIn("cost", body)

    def test_errors(self):
        s = self.serve(
            lambda m, p, b: (404, {"error": 'unknown policy "x"'}) if p == "/v1/check" else (502, b"bad gateway text")
        )
        c = Client(s.url)
        with self.assertRaises(APIError) as cm:
            c.check("k", policy="x")
        self.assertEqual(cm.exception.status, 404)
        self.assertIn('unknown policy "x"', str(cm.exception))
        with self.assertRaises(APIError) as cm:
            c.policies()
        self.assertEqual(cm.exception.status, 502)
        self.assertEqual(cm.exception.message, "bad gateway text")

    def test_reset_policies_healthy(self):
        def responder(m, p, b):
            if p == "/v1/reset":
                return 204, None
            if p == "/v1/policies":
                return 200, {"policies": [{"name": "login", "description": "d", "rules": [{"limit": 5, "window": "1m"}]}]}
            return 503, {"status": "store unavailable"}

        s = self.serve(responder)
        c = Client(s.url)
        c.reset("u", policy="login")
        ps = c.policies()
        self.assertEqual(ps[0].name, "login")
        self.assertEqual(ps[0].rules[0].limit, 5)
        with self.assertRaises(APIError) as cm:
            c.healthy()
        self.assertEqual(cm.exception.status, 503)

    def test_degraded(self):
        s = self.serve(lambda m, p, b: (200, {"allowed": True, "policy": "p", "degraded": True, "error": "store unavailable", "rules": []}))
        r = Client(s.url).check("k", policy="p")
        self.assertTrue(r.allowed and r.degraded)
        self.assertEqual(r.error, "store unavailable")

    def test_timeout(self):
        def slow(m, p, b):
            time.sleep(0.5)
            return 200, {"allowed": True, "rules": []}

        s = self.serve(slow)
        with self.assertRaises(Exception):
            Client(s.url, timeout=0.05).check("k", policy="p")


@unittest.skipUnless(os.environ.get("RATELIMITD_URL"), "RATELIMITD_URL not set")
class IntegrationTest(unittest.TestCase):
    def setUp(self):
        self.c = Client(os.environ["RATELIMITD_URL"], token=os.environ.get("RATELIMITD_TOKEN"))

    def test_flow(self):
        key = f"py-{uuid.uuid4()}"
        rules = [Rule(algorithm="fixed_window", limit=2, window="1m")]
        self.c.healthy()
        for _ in range(2):
            r = self.c.check(key, rules=rules)
            self.assertTrue(r.allowed and r.consumed)
        denied = self.c.check(key, rules=rules)
        self.assertFalse(denied.allowed)
        self.assertGreater(denied.retry_after_ms, 0)
        self.c.reset(key, rules=rules)
        peek = self.c.check(key, rules=rules, peek=True)
        self.assertTrue(peek.allowed and not peek.consumed)

    def test_errors_and_policies(self):
        with self.assertRaises(APIError) as cm:
            self.c.check("k", rules=[Rule(algorithm="fixed_window", limit=1, window="1m")], cost=5)
        self.assertEqual(cm.exception.status, 422)
        with self.assertRaises(APIError) as cm:
            self.c.check("k", policy="nope")
        self.assertEqual(cm.exception.status, 404)
        self.assertIn("sdk_test", [p.name for p in self.c.policies()])


if __name__ == "__main__":
    unittest.main()
