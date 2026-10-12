// Runs against a real ratelimitd; skipped unless RATELIMITD_URL is set.
//   go run ./cmd/ratelimitd -config sdk/test-config.json
//   RATELIMITD_URL=http://127.0.0.1:18080 npm test
import { test } from "node:test";
import assert from "node:assert/strict";
import { Client, RatelimitdError } from "../src/index.js";

const url = process.env.RATELIMITD_URL;
const opts = { skip: url ? false : "RATELIMITD_URL not set" };
const client = () => new Client(url!, { token: process.env.RATELIMITD_TOKEN });

test("inline rules: allow, deny, reset, peek", opts, async () => {
  const c = client();
  const key = `ts-${Date.now()}-${Math.random()}`;
  const rules = [{ algorithm: "fixed_window" as const, limit: 2, window: "1m" }];

  await c.healthy();
  for (let i = 0; i < 2; i++) {
    const r = await c.check({ key, rules });
    assert.ok(r.allowed && r.consumed);
  }
  const denied = await c.check({ key, rules });
  assert.equal(denied.allowed, false);
  assert.ok(denied.retry_after_ms > 0);

  await c.reset({ key, rules });
  const peek = await c.check({ key, rules, peek: true });
  assert.ok(peek.allowed && !peek.consumed);
});

test("validation and unknown policy map to errors", opts, async () => {
  const c = client();
  await assert.rejects(
    c.check({ key: "k", rules: [{ algorithm: "fixed_window", limit: 1, window: "1m" }], cost: 5 }),
    (e: unknown) => e instanceof RatelimitdError && e.status === 422,
  );
  await assert.rejects(c.check({ key: "k", policy: "nope" }), (e: unknown) => e instanceof RatelimitdError && e.status === 404);
});

test("policies lists sdk_test", opts, async () => {
  const ps = await client().policies();
  assert.ok(ps.some((p) => p.name === "sdk_test"));
});
