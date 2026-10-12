package com.phineup.ratelimitd;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.List;
import java.util.concurrent.CompletableFuture;

/** Thread-safe client for a ratelimitd server. */
public final class Client {
    private static final ObjectMapper MAPPER = new ObjectMapper();

    private final String baseUrl;
    private final String token;
    private final HttpClient http;
    private final Duration timeout;

    private Client(Builder b) {
        this.baseUrl = b.baseUrl.replaceAll("/+$", "");
        this.token = b.token;
        this.timeout = b.timeout;
        this.http = b.http != null ? b.http : HttpClient.newBuilder().connectTimeout(Duration.ofSeconds(5)).build();
    }

    public static Builder builder(String baseUrl) {
        return new Builder(baseUrl);
    }

    public static Client create(String baseUrl) {
        return builder(baseUrl).build();
    }

    public static final class Builder {
        private final String baseUrl;
        private String token;
        private HttpClient http;
        private Duration timeout = Duration.ofSeconds(10);

        private Builder(String baseUrl) {
            this.baseUrl = baseUrl;
        }

        public Builder token(String token) {
            this.token = token;
            return this;
        }

        public Builder httpClient(HttpClient http) {
            this.http = http;
            return this;
        }

        /** Per-request timeout (default 10s). */
        public Builder timeout(Duration timeout) {
            this.timeout = timeout;
            return this;
        }

        public Client build() {
            return new Client(this);
        }
    }

    /** Evaluate (and by default consume) a request. */
    public CheckResult check(CheckRequest req) throws IOException, InterruptedException {
        HttpResponse<String> res = send("POST", "/v1/check", req);
        if (res.statusCode() != 200 && res.statusCode() != 429) {
            throw error(res);
        }
        return MAPPER.readValue(res.body(), CheckResult.class);
    }

    public CompletableFuture<CheckResult> checkAsync(CheckRequest req) {
        return CompletableFuture.supplyAsync(() -> {
            try {
                return check(req);
            } catch (IOException e) {
                throw new java.io.UncheckedIOException(e);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                throw new IllegalStateException(e);
            }
        });
    }

    /** Shorthand: check {@code key} against a named policy with cost 1. */
    public boolean allow(String policy, String key) throws IOException, InterruptedException {
        return check(CheckRequest.builder(key).policy(policy).build()).allowed();
    }

    public void reset(CheckRequest req) throws IOException, InterruptedException {
        HttpResponse<String> res = send("POST", "/v1/reset", req);
        if (res.statusCode() != 204 && res.statusCode() != 200) {
            throw error(res);
        }
    }

    public List<Policy> policies() throws IOException, InterruptedException {
        HttpResponse<String> res = send("GET", "/v1/policies", null);
        if (res.statusCode() != 200) {
            throw error(res);
        }
        return MAPPER.readValue(res.body(), PolicyList.class).policies();
    }

    /** Returns normally when healthy, throws {@link RatelimitdException} otherwise. */
    public void healthy() throws IOException, InterruptedException {
        HttpResponse<String> res = send("GET", "/healthz", null);
        if (res.statusCode() != 200) {
            throw error(res);
        }
    }

    @JsonIgnoreProperties(ignoreUnknown = true)
    private record PolicyList(List<Policy> policies) {}

    private HttpResponse<String> send(String method, String path, Object body) throws IOException, InterruptedException {
        HttpRequest.Builder rb = HttpRequest.newBuilder(URI.create(baseUrl + path)).timeout(timeout);
        if (token != null && !token.isEmpty()) {
            rb.header("Authorization", "Bearer " + token);
        }
        if (body != null) {
            rb.header("Content-Type", "application/json")
                    .method(method, HttpRequest.BodyPublishers.ofString(MAPPER.writeValueAsString(body)));
        } else {
            rb.method(method, HttpRequest.BodyPublishers.noBody());
        }
        return http.send(rb.build(), HttpResponse.BodyHandlers.ofString());
    }

    private static RatelimitdException error(HttpResponse<String> res) {
        String msg = res.body() == null ? "" : res.body().trim();
        try {
            JsonNode n = MAPPER.readTree(msg);
            if (n != null && n.hasNonNull("error")) {
                msg = n.get("error").asText();
            }
        } catch (IOException ignored) {
            // not JSON; keep the raw body
        }
        if (msg.isEmpty()) {
            msg = "HTTP " + res.statusCode();
        }
        return new RatelimitdException(res.statusCode(), msg);
    }
}
