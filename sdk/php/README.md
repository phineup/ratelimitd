# ratelimitd — PHP SDK

PHP 8.1+ client for [ratelimitd](https://github.com/phineup/ratelimitd) using `ext-curl` and `ext-json` (no Composer dependencies at runtime).

- Composer package: `phineup/ratelimitd`
- Namespace: `Ratelimitd`

## Install

Composer can install a package directly from a Git repository, so no Packagist account is needed. Add the repository and require it
(the repo root contains a `composer.json` that exposes the PHP SDK):

```bash
composer config repositories.ratelimitd vcs https://github.com/phineup/ratelimitd
composer require phineup/ratelimitd:^1.1
```

That writes this to your `composer.json`:

```json
{
  "repositories": [
    { "type": "vcs", "url": "https://github.com/phineup/ratelimitd" }
  ],
  "require": {
    "phineup/ratelimitd": "^1.1"
  }
}
```

Use `"phineup/ratelimitd": "dev-master"` to track the latest state. Versions come from the repository's `vX.Y.Z` git tags.
Private checkouts work too: `{ "type": "path", "url": "../ratelimitd/sdk/php" }`.

## Usage

```php
<?php
require 'vendor/autoload.php';

use Ratelimitd\Client;

$rl = new Client('http://localhost:8080', token: 'secret', timeout: 2.0); // token only if auth is enabled

// Named policy defined in the server config.
$res = $rl->check(['key' => 'user-42', 'policy' => 'login']);
if (!$res->allowed) {
    // 429 is a normal result, not an exception
    header('Retry-After: ' . (int) ceil($res->retryAfterSeconds()));
    http_response_code(429);
    exit("limited by {$res->limitedBy}");
}

// Inline rules, no server config needed.
$rl->check(['key' => '1.2.3.4', 'rules' => [
    ['algorithm' => 'sliding_window', 'limit' => 10, 'window' => '1m'],
]]);

// Cost and peek (look without consuming).
$rl->check(['key' => 'u', 'policy' => 'login', 'cost' => 3, 'peek' => true]);

// Shorthand, reset, introspection.
$ok = $rl->allow('login', 'user-42');
$rl->reset(['key' => 'user-42', 'policy' => 'login']);
$policies = $rl->policies();   // list of arrays
$rl->healthy();
```

### Laravel middleware sketch

```php
public function handle($request, Closure $next)
{
    try {
        $res = app(\Ratelimitd\Client::class)->check(['key' => $request->ip(), 'policy' => 'api_default']);
        if (!$res->allowed) {
            return response('rate limited', 429)->header('Retry-After', (string) ceil($res->retryAfterSeconds()));
        }
    } catch (\Throwable) {
        // fail open if ratelimitd is unreachable
    }
    return $next($request);
}
```

## API

| method | description |
|---|---|
| `new Client($baseUrl, token: null, timeout: 10.0, transport: null)` | `transport` is an optional closure for custom HTTP |
| `check(array $request): CheckResult` | request keys: `key`, `policy`, `rules`, `cost`, `peek` |
| `allow(string $policy, string $key): bool` | shorthand |
| `reset(array $request): void` | clear counters |
| `policies(): array` | configured policies |
| `healthy(): void` | throws when unhealthy |

`CheckResult` (readonly): `allowed`, `consumed`, `remaining`, `retryAfterMs`, `limitedBy`, `rules`, `policy`, `degraded`, `error`,
and `retryAfterSeconds()`. Non-2xx responses other than 429 throw `ApiException` (`->status`); network failures throw `TransportException`.

## Test

```bash
cd sdk/php && composer install && vendor/bin/phpunit
RATELIMITD_URL=http://127.0.0.1:18080 vendor/bin/phpunit
```
