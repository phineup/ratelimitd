package com.phineup.ratelimitd;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;

@JsonIgnoreProperties(ignoreUnknown = true)
public record RuleResult(
        String name,
        String algorithm,
        boolean allowed,
        long limit,
        long remaining,
        @JsonProperty("retry_after_ms") long retryAfterMs,
        @JsonProperty("reset_after_ms") long resetAfterMs) {}
