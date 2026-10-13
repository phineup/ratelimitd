package com.phineup.ratelimitd;

import com.fasterxml.jackson.annotation.JsonInclude;
import java.util.List;

/** Set either {@code policy} or {@code rules}. Use {@link #builder(String)}. */
@JsonInclude(JsonInclude.Include.NON_DEFAULT)
public record CheckRequest(String key, String policy, List<Rule> rules, long cost, boolean peek) {

    public static Builder builder(String key) {
        return new Builder(key);
    }

    public static final class Builder {
        private final String key;
        private String policy;
        private List<Rule> rules;
        private long cost;
        private boolean peek;

        private Builder(String key) {
            this.key = key;
        }

        public Builder policy(String policy) {
            this.policy = policy;
            return this;
        }

        public Builder rules(List<Rule> rules) {
            this.rules = rules;
            return this;
        }

        /** Defaults to 1 on the server when unset. */
        public Builder cost(long cost) {
            this.cost = cost;
            return this;
        }

        /** Evaluate without consuming quota. */
        public Builder peek(boolean peek) {
            this.peek = peek;
            return this;
        }

        public CheckRequest build() {
            return new CheckRequest(key, policy, rules, cost, peek);
        }
    }
}
