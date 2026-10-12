package com.phineup.ratelimitd;

import static org.junit.jupiter.api.Assertions.*;

import java.util.List;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.condition.EnabledIfEnvironmentVariable;

/**
 * Runs against a real ratelimitd when RATELIMITD_URL is set:
 * {@code go run ./cmd/ratelimitd -config sdk/test-config.json}.
 */
@EnabledIfEnvironmentVariable(named = "RATELIMITD_URL", matches = ".+")
class IntegrationTest {
    private Client client() {
        String token = System.getenv("RATELIMITD_TOKEN");
        return Client.builder(System.getenv("RATELIMITD_URL")).token(token).build();
    }

    @Test
    void flow() throws Exception {
        Client c = client();
        String key = "java-" + UUID.randomUUID();
        List<Rule> rules = List.of(Rule.of("fixed_window", 2, "1m"));

        c.healthy();
        for (int i = 0; i < 2; i++) {
            CheckResult r = c.check(CheckRequest.builder(key).rules(rules).build());
            assertTrue(r.allowed() && r.consumed());
        }
        CheckResult denied = c.check(CheckRequest.builder(key).rules(rules).build());
        assertFalse(denied.allowed());
        assertTrue(denied.retryAfterMs() > 0);

        c.reset(CheckRequest.builder(key).rules(rules).build());
        CheckResult peek = c.check(CheckRequest.builder(key).rules(rules).peek(true).build());
        assertTrue(peek.allowed());
        assertFalse(peek.consumed());
    }

    @Test
    void errorsAndPolicies() throws Exception {
        Client c = client();
        assertEquals(422, assertThrows(RatelimitdException.class,
                () -> c.check(CheckRequest.builder("k").rules(List.of(Rule.of("fixed_window", 1, "1m"))).cost(5).build())).status());
        assertEquals(404, assertThrows(RatelimitdException.class,
                () -> c.check(CheckRequest.builder("k").policy("nope").build())).status());
        assertTrue(c.policies().stream().anyMatch(p -> p.name().equals("sdk_test")));
    }
}
