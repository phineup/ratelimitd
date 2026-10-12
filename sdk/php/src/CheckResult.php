<?php

declare(strict_types=1);

namespace Ratelimitd;

/** Decision from Client::check(). A denial (HTTP 429) is a normal result. */
final class CheckResult
{
    /**
     * @param list<array<string,mixed>> $rules per-rule results, as returned by the server
     */
    public function __construct(
        public readonly bool $allowed,
        public readonly bool $consumed,
        public readonly int $remaining,
        public readonly int $retryAfterMs,
        public readonly ?string $limitedBy,
        public readonly array $rules,
        public readonly string $policy,
        /** True when the server's store was down and it failed open. */
        public readonly bool $degraded,
        public readonly ?string $error,
    ) {
    }

    /** @param array<string,mixed> $d */
    public static function fromArray(array $d): self
    {
        return new self(
            (bool) ($d['allowed'] ?? false),
            (bool) ($d['consumed'] ?? false),
            (int) ($d['remaining'] ?? 0),
            (int) ($d['retry_after_ms'] ?? 0),
            isset($d['limited_by']) ? (string) $d['limited_by'] : null,
            is_array($d['rules'] ?? null) ? array_values($d['rules']) : [],
            (string) ($d['policy'] ?? ''),
            (bool) ($d['degraded'] ?? false),
            isset($d['error']) ? (string) $d['error'] : null,
        );
    }

    public function retryAfterSeconds(): float
    {
        return $this->retryAfterMs / 1000;
    }
}
