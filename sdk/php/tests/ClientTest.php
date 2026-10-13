<?php

declare(strict_types=1);

namespace Ratelimitd\Tests;

use PHPUnit\Framework\TestCase;
use Ratelimitd\ApiException;
use Ratelimitd\Client;
use Ratelimitd\TransportException;

final class ClientTest extends TestCase
{
    /** @var list<array{method:string,url:string,headers:array<string,string>,body:?string}> */
    private array $seen = [];

    /** @param callable(string,?string):array{0:int,1:string} $respond */
    private function client(callable $respond, ?string $token = null): Client
    {
        return new Client(
            'http://rl.test/',
            $token,
            1.0,
            function (string $method, string $url, array $headers, ?string $body) use ($respond): array {
                $this->seen[] = compact('method', 'url', 'headers', 'body');
                return $respond($url, $body);
            },
        );
    }

    public function testCheckEncodesRequestAndDecodesAllowed(): void
    {
        $c = $this->client(fn () => [200, '{"allowed":true,"consumed":true,"remaining":4,"retry_after_ms":0,"policy":"login","rules":[],"future":1}'], 'tok');
        $r = $c->check(['key' => 'u', 'policy' => 'login', 'cost' => 2, 'peek' => true]);

        $this->assertTrue($r->allowed);
        $this->assertTrue($r->consumed);
        $this->assertSame(4, $r->remaining);
        $this->assertSame('login', $r->policy);

        $s = $this->seen[0];
        $this->assertSame('POST', $s['method']);
        $this->assertSame('http://rl.test/v1/check', $s['url']);
        $this->assertSame('Bearer tok', $s['headers']['Authorization']);
        $this->assertSame('application/json', $s['headers']['Content-Type']);
        $this->assertSame(['key' => 'u', 'policy' => 'login', 'cost' => 2, 'peek' => true], json_decode((string) $s['body'], true));
    }

    public function testDeniedIsAResultNotAnException(): void
    {
        $c = $this->client(fn () => [429, '{"allowed":false,"consumed":false,"remaining":0,"retry_after_ms":1500,"limited_by":"r","policy":"p","rules":[{"name":"r","retry_after_ms":1500}]}']);
        $r = $c->check(['key' => 'u', 'policy' => 'p']);

        $this->assertFalse($r->allowed);
        $this->assertSame(1500, $r->retryAfterMs);
        $this->assertSame(1.5, $r->retryAfterSeconds());
        $this->assertSame('r', $r->limitedBy);
        $this->assertCount(1, $r->rules);
        $this->assertFalse($c->allow('p', 'u'));
    }

    public function testErrorStatusesBecomeExceptions(): void
    {
        $c = $this->client(fn (string $url) => str_ends_with($url, '/v1/check')
            ? [404, '{"error":"unknown policy \"x\""}']
            : [502, 'bad gateway text']);

        try {
            $c->check(['key' => 'k', 'policy' => 'x']);
            $this->fail('expected ApiException');
        } catch (ApiException $e) {
            $this->assertSame(404, $e->status);
            $this->assertSame('unknown policy "x"', $e->reason);
        }
        try {
            $c->policies();
            $this->fail('expected ApiException');
        } catch (ApiException $e) {
            $this->assertSame(502, $e->status);
            $this->assertSame('bad gateway text', $e->reason);
        }
    }

    public function testResetPoliciesHealthy(): void
    {
        $c = $this->client(function (string $url) {
            if (str_ends_with($url, '/v1/reset')) {
                return [204, ''];
            }
            if (str_ends_with($url, '/v1/policies')) {
                return [200, '{"policies":[{"name":"login","rules":[{"limit":5,"window":"1m"}]}]}'];
            }
            return [503, '{"status":"store unavailable"}'];
        });
        $c->reset(['key' => 'u', 'policy' => 'login']);
        $ps = $c->policies();
        $this->assertSame('login', $ps[0]['name']);
        try {
            $c->healthy();
            $this->fail('expected ApiException');
        } catch (ApiException $e) {
            $this->assertSame(503, $e->status);
        }
    }

    public function testDegradedAndMalformed(): void
    {
        $c = $this->client(fn () => [200, '{"allowed":true,"policy":"p","degraded":true,"error":"store unavailable","rules":[]}']);
        $r = $c->check(['key' => 'k', 'policy' => 'p']);
        $this->assertTrue($r->allowed);
        $this->assertTrue($r->degraded);
        $this->assertSame('store unavailable', $r->error);

        $bad = $this->client(fn () => [200, 'not json']);
        $this->expectException(TransportException::class);
        $bad->check(['key' => 'k', 'policy' => 'p']);
    }

    public function testCurlTransportReportsConnectionFailure(): void
    {
        $sock = stream_socket_server('tcp://127.0.0.1:0');
        $name = stream_socket_get_name($sock, false);
        fclose($sock); // port now has nothing listening
        $c = new Client('http://' . $name, null, 1.0);
        $this->expectException(TransportException::class);
        $c->healthy();
    }

    public function testIntegration(): void
    {
        $url = getenv('RATELIMITD_URL');
        if ($url === false || $url === '') {
            $this->markTestSkipped('RATELIMITD_URL not set');
        }
        $c = new Client($url, getenv('RATELIMITD_TOKEN') ?: null);
        $key = 'php-' . bin2hex(random_bytes(8));
        $rules = [['algorithm' => 'fixed_window', 'limit' => 2, 'window' => '1m']];

        $c->healthy();
        for ($i = 0; $i < 2; $i++) {
            $r = $c->check(['key' => $key, 'rules' => $rules]);
            $this->assertTrue($r->allowed && $r->consumed);
        }
        $denied = $c->check(['key' => $key, 'rules' => $rules]);
        $this->assertFalse($denied->allowed);
        $this->assertGreaterThan(0, $denied->retryAfterMs);

        $c->reset(['key' => $key, 'rules' => $rules]);
        $peek = $c->check(['key' => $key, 'rules' => $rules, 'peek' => true]);
        $this->assertTrue($peek->allowed);
        $this->assertFalse($peek->consumed);

        try {
            $c->check(['key' => 'k', 'rules' => $rules, 'cost' => 5]);
            $this->fail('expected 422');
        } catch (ApiException $e) {
            $this->assertSame(422, $e->status);
        }
        $names = array_column($c->policies(), 'name');
        $this->assertContains('sdk_test', $names);
    }
}
