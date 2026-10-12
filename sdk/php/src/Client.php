<?php

declare(strict_types=1);

namespace Ratelimitd;

/**
 * Client for the ratelimitd rate limiting sidecar (ext-curl).
 *
 * Requests are plain arrays so every server field is available:
 *   $client->check(['key' => 'user-42', 'policy' => 'login']);
 *   $client->check(['key' => '1.2.3.4', 'rules' => [['algorithm' => 'sliding_window', 'limit' => 10, 'window' => '1m']]]);
 */
final class Client
{
    private string $baseUrl;

    /**
     * @param \Closure(string,string,array<string,string>,?string,float):array{0:int,1:string}|null $transport
     *        (method, url, headers, body, timeoutSeconds) => [status, body]. Defaults to curl.
     */
    public function __construct(
        string $baseUrl,
        private readonly ?string $token = null,
        private readonly float $timeout = 10.0,
        private readonly ?\Closure $transport = null,
    ) {
        $this->baseUrl = rtrim($baseUrl, '/');
    }

    /**
     * @param array{key:string,policy?:string,rules?:list<array<string,mixed>>,cost?:int,peek?:bool} $request
     */
    public function check(array $request): CheckResult
    {
        [$status, $body] = $this->send('POST', '/v1/check', $request);
        if ($status !== 200 && $status !== 429) {
            throw self::error($status, $body);
        }
        return CheckResult::fromArray(self::decode($body));
    }

    /** Shorthand: check $key against a named policy with cost 1. */
    public function allow(string $policy, string $key): bool
    {
        return $this->check(['key' => $key, 'policy' => $policy])->allowed;
    }

    /** @param array{key:string,policy?:string,rules?:list<array<string,mixed>>} $request */
    public function reset(array $request): void
    {
        [$status, $body] = $this->send('POST', '/v1/reset', $request);
        if ($status !== 204 && $status !== 200) {
            throw self::error($status, $body);
        }
    }

    /** @return list<array<string,mixed>> */
    public function policies(): array
    {
        [$status, $body] = $this->send('GET', '/v1/policies', null);
        if ($status !== 200) {
            throw self::error($status, $body);
        }
        $d = self::decode($body);
        return array_values($d['policies'] ?? []);
    }

    /** Returns normally when healthy; throws ApiException otherwise. */
    public function healthy(): void
    {
        [$status, $body] = $this->send('GET', '/healthz', null);
        if ($status !== 200) {
            throw self::error($status, $body);
        }
    }

    /**
     * @param array<string,mixed>|null $payload
     * @return array{0:int,1:string}
     */
    private function send(string $method, string $path, ?array $payload): array
    {
        $headers = [];
        $body = null;
        if ($payload !== null) {
            $body = json_encode($payload, JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES);
            $headers['Content-Type'] = 'application/json';
        }
        if ($this->token !== null && $this->token !== '') {
            $headers['Authorization'] = 'Bearer ' . $this->token;
        }
        $url = $this->baseUrl . $path;
        if ($this->transport !== null) {
            return ($this->transport)($method, $url, $headers, $body, $this->timeout);
        }
        return self::curl($method, $url, $headers, $body, $this->timeout);
    }

    /**
     * @param array<string,string> $headers
     * @return array{0:int,1:string}
     */
    private static function curl(string $method, string $url, array $headers, ?string $body, float $timeout): array
    {
        $ch = curl_init($url);
        if ($ch === false) {
            throw new TransportException('ratelimitd: curl_init failed');
        }
        $lines = [];
        foreach ($headers as $k => $v) {
            $lines[] = "$k: $v";
        }
        curl_setopt_array($ch, [
            CURLOPT_CUSTOMREQUEST => $method,
            CURLOPT_HTTPHEADER => $lines,
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_TIMEOUT_MS => (int) ($timeout * 1000),
        ]);
        if ($body !== null) {
            curl_setopt($ch, CURLOPT_POSTFIELDS, $body);
        }
        $out = curl_exec($ch);
        if ($out === false) {
            $err = curl_error($ch);
            curl_close($ch);
            throw new TransportException('ratelimitd: ' . $err);
        }
        $status = (int) curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
        curl_close($ch);
        return [$status, (string) $out];
    }

    /** @return array<string,mixed> */
    private static function decode(string $body): array
    {
        try {
            $d = json_decode($body, true, 64, JSON_THROW_ON_ERROR);
        } catch (\JsonException $e) {
            throw new TransportException('ratelimitd: decode: ' . $e->getMessage(), 0, $e);
        }
        if (!is_array($d)) {
            throw new TransportException('ratelimitd: decode: expected a JSON object');
        }
        return $d;
    }

    private static function error(int $status, string $body): ApiException
    {
        $msg = trim($body);
        $d = json_decode($body, true);
        if (is_array($d) && isset($d['error']) && is_string($d['error']) && $d['error'] !== '') {
            $msg = $d['error'];
        }
        return new ApiException($status, $msg !== '' ? $msg : "HTTP $status");
    }
}
