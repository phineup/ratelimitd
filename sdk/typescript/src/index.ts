/**
 * Client for the ratelimitd rate limiting sidecar.
 * Zero runtime dependencies; uses the global `fetch` (Node >= 18, Deno, Bun, browsers).
 */

export type Algorithm = "fixed_window" | "sliding_window" | "sliding_log" | "token_bucket";
export type Anchor = "clock" | "first_request";

/** An inline rule. `window` uses the server syntax: "30s", "5h", "1d12h". */
export interface Rule {
  name?: string;
  algorithm?: Algorithm;
  limit: number;
  window: string;
  burst?: number;
  anchor?: Anchor;
}

export interface Policy {
  name: string;
  description?: string;
  rules: Rule[];
  dynamic?: boolean;
}

/** Set either `policy` or `rules`. */
export interface CheckRequest {
  key: string;
  policy?: string;
  rules?: Rule[];
  /** Defaults to 1 on the server. */
  cost?: number;
  /** Evaluate without consuming quota. */
  peek?: boolean;
}

export interface RuleResult {
  name: string;
  algorithm: Algorithm;
  allowed: boolean;
  limit: number;
  remaining: number;
  retry_after_ms: number;
  reset_after_ms: number;
}

export interface CheckResult {
  allowed: boolean;
  consumed: boolean;
  remaining: number;
  retry_after_ms: number;
  limited_by?: string;
  rules: RuleResult[];
  policy: string;
  /** True when the server's store was down and it failed open. */
  degraded?: boolean;
  error?: string;
}

/** Thrown for non-2xx responses other than 429 (a denial is a normal result). */
export class RatelimitdError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(`ratelimitd: HTTP ${status}: ${message}`);
    this.name = "RatelimitdError";
    this.status = status;
  }
}

export interface ClientOptions {
  /** Bearer token, when the server has auth enabled. */
  token?: string;
  /** Custom fetch implementation (defaults to global fetch). */
  fetch?: typeof fetch;
  /** Per-request timeout in milliseconds (default 10000, 0 disables). */
  timeoutMs?: number;
}

export class Client {
  private readonly baseUrl: string;
  private readonly token?: string;
  private readonly fetchImpl: typeof fetch;
  private readonly timeoutMs: number;

  constructor(baseUrl: string, opts: ClientOptions = {}) {
    this.baseUrl = baseUrl.replace(/\/+$/, "");
    this.token = opts.token;
    this.fetchImpl = opts.fetch ?? globalThis.fetch.bind(globalThis);
    this.timeoutMs = opts.timeoutMs ?? 10_000;
  }

  /** Evaluate (and by default consume) a request. A denial resolves with `allowed: false`. */
  async check(req: CheckRequest, signal?: AbortSignal): Promise<CheckResult> {
    const res = await this.request("POST", "/v1/check", req, signal);
    if (res.status !== 200 && res.status !== 429) throw await toError(res);
    return (await res.json()) as CheckResult;
  }

  /** Shorthand: check `key` against a named policy with cost 1. */
  async allow(policy: string, key: string, signal?: AbortSignal): Promise<boolean> {
    return (await this.check({ key, policy }, signal)).allowed;
  }

  /** Clear counters for `key` under a policy or inline rules. */
  async reset(req: CheckRequest, signal?: AbortSignal): Promise<void> {
    const res = await this.request("POST", "/v1/reset", req, signal);
    if (res.status !== 204 && res.status !== 200) throw await toError(res);
  }

  async policies(signal?: AbortSignal): Promise<Policy[]> {
    const res = await this.request("GET", "/v1/policies", undefined, signal);
    if (res.status !== 200) throw await toError(res);
    return ((await res.json()) as { policies: Policy[] }).policies;
  }

  /** Resolves when the server and its store are healthy, rejects otherwise. */
  async healthy(signal?: AbortSignal): Promise<void> {
    const res = await this.request("GET", "/healthz", undefined, signal);
    if (res.status !== 200) throw await toError(res);
  }

  private async request(method: string, path: string, body: unknown, signal?: AbortSignal): Promise<Response> {
    const headers: Record<string, string> = {};
    if (body !== undefined) headers["Content-Type"] = "application/json";
    if (this.token) headers["Authorization"] = `Bearer ${this.token}`;

    const signals: AbortSignal[] = [];
    if (signal) signals.push(signal);
    if (this.timeoutMs > 0) signals.push(AbortSignal.timeout(this.timeoutMs));

    return this.fetchImpl(this.baseUrl + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: signals.length ? AbortSignal.any(signals) : undefined,
    });
  }
}

async function toError(res: Response): Promise<RatelimitdError> {
  const text = await res.text();
  let msg = text.trim();
  try {
    const parsed = JSON.parse(text) as { error?: string };
    if (parsed.error) msg = parsed.error;
  } catch {
    /* not JSON */
  }
  return new RatelimitdError(res.status, msg || res.statusText);
}
