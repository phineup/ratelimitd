# @phineup/ratelimitd

TypeScript / JavaScript client for [ratelimitd](https://github.com/phineup/ratelimitd). No runtime dependencies; uses the global `fetch` (Node >= 18, Deno, Bun, browsers). Ships ESM, CommonJS and type declarations.

Published to GitHub Packages (not npmjs). Add to your `.npmrc`:

```
@phineup:registry=https://npm.pkg.github.com
```

```ts
import { Client } from "@phineup/ratelimitd";

const rl = new Client("http://localhost:8080", { token: "secret" });
const res = await rl.check({ key: "user-42", policy: "login" });
if (!res.allowed) {
  // 429 is a normal result, not an exception
  console.log(`retry in ${res.retry_after_ms} ms`);
}
```

JavaScript works the same (`const { Client } = require("@phineup/ratelimitd")` or `import`).
Methods: `check`, `allow`, `reset`, `policies`, `healthy`. Non-2xx responses other than 429 throw `RatelimitdError` (`.status`).

```
npm ci && npm test                      # unit tests
RATELIMITD_URL=http://127.0.0.1:18080 npm test   # + real-server tests
```
