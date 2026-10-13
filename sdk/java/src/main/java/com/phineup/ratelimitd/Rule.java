package com.phineup.ratelimitd;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonInclude;

/** An inline rule. {@code window} uses the server syntax: "30s", "5h", "1d12h". */
@JsonInclude(JsonInclude.Include.NON_NULL)
@JsonIgnoreProperties(ignoreUnknown = true)
public record Rule(String name, String algorithm, long limit, String window, Long burst, String anchor) {

    public static Rule of(String algorithm, long limit, String window) {
        return new Rule(null, algorithm, limit, window, null, null);
    }

    public Rule withBurst(long burst) {
        return new Rule(name, algorithm, limit, window, burst, anchor);
    }
}
