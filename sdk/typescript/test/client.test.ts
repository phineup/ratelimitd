import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import { Client, RatelimitdError } from "../src/index.js";

type Handler = (req: IncomingMessage, body: string, res: ServerResponse) => void;

async function withServer(handler: Handler, fn: (url: string) => Promise<void>): Promise<void> {
  const server = createServer((req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => handler(req, body, res));
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  const { port } = server.address() as AddressInfo;
  try {
    await fn(`http://127.0.0.1:${port}`);
  } finally {
    server.closeAllConnections();
    await new Promise((r) => server.close(r));
  }
}

const json = (res: ServerResponse, status: number, v: unknown) => {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(v));
};

test("check encodes the request and decodes an allowed result", async () => {
  let seen: { url?: string; auth?: string; ct?: string; body?: any } = {};
  await withServer(
    (req, body, res) => {
      seen = { url: req.url, auth: req.headers.authorization, ct: req.headers["content-type"], body: JSON.parse(body) };
      json(res, 200, { allowed: true, consumed: true, remaining: 4, retry_after_ms: 0, rules: [], policy: "login" });
    },
    async (url) => {
      const c = new Client(url + "/", { token: "tok" });
      const r = await c.check({ key: "u", policy: "login", cost: 2, peek: true });
      assert.equal(r.allowed, true);
      assert.equal(r.remaining, 4);
      assert.equal(seen.url, "/v1/check");
      assert.equal(seen.auth, "Bearer tok");
      assert.equal(seen.ct, "application/json");
      assert.deepEqual(seen.body, { key: "u", policy: "login", cost: 2, peek: true });
    },
  );
});

test("429 resolves as a denial, not an error", async () => {
  await withServer(
    (_r, _b, res) => json(res, 429, { allowed: false, consumed: false, remaining: 0, retry_after_ms: 1500, limited_by: "r", rules: [], policy: "p" }),
    async (url) => {
      const c = new Client(url);
      const r = await c.check({ key: "u", policy: "p" });
      assert.equal(r.allowed, false);
      assert.equal(r.retry_after_ms, 1500);
      assert.equal(r.limited_by, "r");
      assert.equal(await c.allow("p", "u"), false);
    },
  );
});

test("inline rules are sent verbatim", async () => {
  let body: any;
  await withServer(
    (_r, b, res) => {
      body = JSON.parse(b);
      json(res, 200, { allowed: true, rules: [], policy: "inline" });
    },
    async (url) => {
      await new Client(url).check({ key: "1.2.3.4", rules: [{ algorithm: "token_bucket", limit: 10, window: "1m", burst: 20 }] });
      assert.deepEqual(body.rules, [{ algorithm: "token_bucket", limit: 10, window: "1m", burst: 20 }]);
      assert.equal("policy" in body, false);
    },
  );
});

test("error statuses become RatelimitdError with the server message", async () => {
  await withServer(
    (req, _b, res) => {
      if (req.url === "/v1/check") return json(res, 404, { error: 'unknown policy "x"' });
      res.writeHead(502).end("bad gateway text");
    },
    async (url) => {
      const c = new Client(url);
      await assert.rejects(c.check({ key: "k", policy: "x" }), (e: unknown) => {
        assert.ok(e instanceof RatelimitdError);
        assert.equal(e.status, 404);
        assert.match(e.message, /unknown policy "x"/);
        return true;
      });
      await assert.rejects(c.policies(), (e: unknown) => e instanceof RatelimitdError && e.status === 502 && /bad gateway text/.test(e.message));
    },
  );
});

test("reset, policies and healthy", async () => {
  await withServer(
    (req, _b, res) => {
      if (req.url === "/v1/reset") return void res.writeHead(204).end();
      if (req.url === "/v1/policies") return json(res, 200, { policies: [{ name: "login", rules: [{ limit: 5, window: "1m" }] }] });
      if (req.url === "/healthz") return json(res, 503, { status: "store unavailable" });
      res.writeHead(404).end();
    },
    async (url) => {
      const c = new Client(url);
      await c.reset({ key: "u", policy: "login" });
      const ps = await c.policies();
      assert.equal(ps[0]?.name, "login");
      await assert.rejects(c.healthy(), (e: unknown) => e instanceof RatelimitdError && e.status === 503);
    },
  );
});

test("timeout aborts a hung request", async () => {
  await withServer(
    () => {
      /* never respond */
    },
    async (url) => {
      const c = new Client(url, { timeoutMs: 50 });
      await assert.rejects(c.check({ key: "k", policy: "p" }));
    },
  );
});

test("caller AbortSignal aborts the request", async () => {
  await withServer(
    () => {},
    async (url) => {
      const ac = new AbortController();
      const c = new Client(url, { timeoutMs: 0 });
      const p = c.check({ key: "k", policy: "p" }, ac.signal);
      ac.abort();
      await assert.rejects(p);
    },
  );
});

test("custom fetch implementation is used", async () => {
  let called = false;
  const c = new Client("http://example.invalid", {
    fetch: (async () => {
      called = true;
      return new Response(JSON.stringify({ allowed: true, rules: [], policy: "p" }), { status: 200 });
    }) as typeof fetch,
  });
  assert.equal(await c.allow("p", "k"), true);
  assert.equal(called, true);
});
