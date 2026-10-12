<?php

declare(strict_types=1);

namespace Ratelimitd;

/** Non-2xx response other than 429 (a denial is a normal CheckResult). */
final class ApiException extends \RuntimeException
{
    public function __construct(public readonly int $status, public readonly string $reason)
    {
        parent::__construct("ratelimitd: HTTP {$status}: {$reason}", $status);
    }
}
