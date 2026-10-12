package com.phineup.ratelimitd;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;
import java.time.Duration;
import java.util.List;

/** Decision returned by {@link Client#check}. A denial (HTTP 429) is a normal result. */
@JsonIgnoreProperties(ignoreUnknown = true)
public record CheckResult(
        boolean allowed,
        boolean consumed,
        long remaining,
        @JsonProperty("retry_after_ms") long retryAfterMs,
        @JsonProperty("limited_by") String limitedBy,
        List<RuleResult> rules,
        String policy,
        /** True when the server's store was down and it failed open. */
        boolean degraded,
        String error) {

    public Duration retryAfter() {
        return Duration.ofMillis(retryAfterMs);
    }
}
