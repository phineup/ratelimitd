# ratelimitd (PHP)

PHP 8.1+ client for [ratelimitd](https://github.com/phineup/ratelimitd) using ext-curl.

```php
$rl = new Ratelimitd\Client('http://localhost:8080', token: 'secret');
$res = $rl->check(['key' => 'user-42', 'policy' => 'login']);
if (!$res->allowed) {
    // 429 is a normal result; $res->retryAfterSeconds()
}
```

Methods: `check`, `allow`, `reset`, `policies`, `healthy`. Other non-2xx responses throw `ApiException` (`->status`).

```
composer install && vendor/bin/phpunit
RATELIMITD_URL=http://127.0.0.1:18080 vendor/bin/phpunit
```
