# @phineup/ratelimitd — TypeScript / JavaScript SDK

TypeScript / JavaScript client for [ratelimitd](https://github.com/phineup/ratelimitd). No runtime dependencies; uses the
global `fetch` (Node >= 18, Deno, Bun, browsers). Ships ESM, CommonJS and type declarations.

## Install

The package is published to **GitHub Packages** (not npmjs). GitHub Packages requires authentication even for reads,
so create a personal access token with the `read:packages` scope and put this in your project's (or `~/`) `.npmrc`:

```ini
@phineup:registry=https://npm.pkg.github.com
//npm.pkg.github.com/:_authToken=${GITHUB_TOKEN}
```

```bash
export GITHUB_TOKEN=ghp_xxx      # PAT with read:packages (in GitHub Actions: secrets.GITHUB_TOKEN)
npm install @phineup/ratelimitd  # or: pnpm add / yarn add / bun add
```

Building from a checkout instead:

```bash
cd sdk/typescript && npm ci && npm run build   # outputs dist/esm and dist/cjs
npm install /path/to/ratelimitd/sdk/typescript
```

## Usage

```ts
import { Client } from "@phineup/ratelimitd";

const rl = new Client("http://localhost:8080", {
  token: "secret",      // only when the server has auth enabled
  timeoutMs: 2000,      // default 10000, 0 disables
});

// Named policy defined in the server config.
const res = await rl.check({ key: "user-42", policy: "login" });
if (!res.allowed) {
  // 429 is a normal result, not an exception
  console.log(`limited by ${res.limited_by}, retry in ${res.retry_after_ms} ms`);
}

// Inline rules, no server config needed.
await rl.check({ key: "1.2.3.4", rules: [{ algorithm: "sliding_window", limit: 10, window: "1m" }] });

// Cost and peek (look without consuming).
await rl.check({ key: "u", policy: "login", cost: 3, peek: true });

// Shorthand, reset, introspection.
const ok = await rl.allow("login", "user-42");
await rl.reset({ key: "user-42", policy: "login" });
const policies = await rl.policies();
await rl.healthy();
```

CommonJS / plain JavaScript:

```js
const { Client } = require("@phineup/ratelimitd");
const rl = new Client("http://localhost:8080");
rl.allow("login", "user-42").then(console.log);
```

### Express middleware sketch

```ts
app.use(async (req, res, next) => {
  try {
    const r = await rl.check({ key: req.ip!, policy: "api_default" });
    if (!r.allowed) {
      res.set("Retry-After", String(Math.ceil(r.retry_after_ms / 1000)));
      return res.status(429).json({ error: "rate limited" });
    }
  } catch {
    // fail open if ratelimitd is unreachable
  }
  next();
});
```

## API

| method | description |
|---|---|
| `new Client(baseUrl, opts?)` | `opts`: `token`, `fetch`, `timeoutMs` |
| `check(req, signal?)` | evaluate and (unless `peek`) consume; resolves with `allowed: false` on 429 |
| `allow(policy, key, signal?)` | shorthand returning a boolean |
| `reset(req, signal?)` | clear counters for a key |
| `policies(signal?)` | list configured policies |
| `healthy(signal?)` | resolves when `/healthz` is OK |

Result fields (snake_case, as on the wire): `allowed`, `consumed`, `remaining`, `retry_after_ms`, `limited_by`, `rules[]`,
`policy`, `degraded`, `error`. Non-2xx responses other than 429 throw `RatelimitdError` (`.status`).

## Test

```bash
npm ci && npm test                                  # unit tests
RATELIMITD_URL=http://127.0.0.1:18080 npm test      # + real-server tests
```
