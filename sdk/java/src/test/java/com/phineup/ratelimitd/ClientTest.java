package com.phineup.ratelimitd;

import static org.junit.jupiter.api.Assertions.*;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.List;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.BiFunction;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

class ClientTest {
    private static final ObjectMapper M = new ObjectMapper();
    private HttpServer server;

    record Seen(String method, String path, String auth, String contentType, String body) {}

    private final AtomicReference<Seen> seen = new AtomicReference<>();

    /** responder: (path, body) -> {status, payload}; payload null = empty body. */
    private String serve(BiFunction<String, String, Object[]> responder) throws IOException {
        server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/", ex -> {
            String body = new String(ex.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
            seen.set(new Seen(
                    ex.getRequestMethod(),
                    ex.getRequestURI().getPath(),
                    ex.getRequestHeaders().getFirst("Authorization"),
                    ex.getRequestHeaders().getFirst("Content-Type"),
                    body));
            Object[] r = responder.apply(ex.getRequestURI().getPath(), body);
            int status = (Integer) r[0];
            byte[] out = r[1] == null ? new byte[0] : ((String) r[1]).getBytes(StandardCharsets.UTF_8);
            ex.sendResponseHeaders(status, status == 204 || out.length == 0 ? -1 : out.length);
            if (out.length > 0 && status != 204) {
                ex.getResponseBody().write(out);
            }
            ex.close();
        });
        server.start();
        return "http://127.0.0.1:" + server.getAddress().getPort();
    }

    @AfterEach
    void stop() {
        if (server != null) {
            server.stop(0);
        }
    }

    @Test
    void checkEncodesRequestAndDecodesAllowed() throws Exception {
        String url = serve((p, b) -> new Object[] {
            200, "{\"allowed\":true,\"consumed\":true,\"remaining\":4,\"retry_after_ms\":0,\"policy\":\"login\",\"rules\":[],\"future_field\":1}"
        });
        Client c = Client.builder(url + "/").token("tok").build();
        CheckResult r = c.check(CheckRequest.builder("u").policy("login").cost(2).peek(true).build());

        assertTrue(r.allowed());
        assertEquals(4, r.remaining());
        assertEquals("login", r.policy());
        Seen s = seen.get();
        assertEquals("/v1/check", s.path());
        assertEquals("Bearer tok", s.auth());
        assertEquals("application/json", s.contentType());
        JsonNode body = M.readTree(s.body());
        assertEquals("u", body.get("key").asText());
        assertEquals("login", body.get("policy").asText());
        assertEquals(2, body.get("cost").asInt());
        assertTrue(body.get("peek").asBoolean());
        assertFalse(body.has("rules"));
    }

    @Test
    void deniedIsAResultNotAnException() throws Exception {
        String url = serve((p, b) -> new Object[] {
            429, "{\"allowed\":false,\"consumed\":false,\"remaining\":0,\"retry_after_ms\":1500,\"limited_by\":\"r\",\"policy\":\"p\","
                    + "\"rules\":[{\"name\":\"r\",\"algorithm\":\"fixed_window\",\"allowed\":false,\"limit\":1,\"remaining\":0,\"retry_after_ms\":1500,\"reset_after_ms\":1500}]}"
        });
        Client c = Client.create(url);
        CheckResult r = c.check(CheckRequest.builder("u").policy("p").build());
        assertFalse(r.allowed());
        assertEquals(Duration.ofMillis(1500), r.retryAfter());
        assertEquals("r", r.limitedBy());
        assertEquals(1500, r.rules().get(0).retryAfterMs());
        assertFalse(c.allow("p", "u"));
    }

    @Test
    void inlineRulesOmitUnsetFields() throws Exception {
        String url = serve((p, b) -> new Object[] {200, "{\"allowed\":true,\"rules\":[],\"policy\":\"inline\"}"});
        Client.create(url).check(CheckRequest.builder("1.2.3.4")
                .rules(List.of(Rule.of("token_bucket", 10, "1m").withBurst(20)))
                .build());
        JsonNode rule = M.readTree(seen.get().body()).get("rules").get(0);
        assertEquals("token_bucket", rule.get("algorithm").asText());
        assertEquals(10, rule.get("limit").asInt());
        assertEquals(20, rule.get("burst").asInt());
        assertFalse(rule.has("name"));
        assertFalse(rule.has("anchor"));
    }

    @Test
    void errorStatusesBecomeExceptions() throws Exception {
        String url = serve((p, b) -> p.equals("/v1/check")
                ? new Object[] {404, "{\"error\":\"unknown policy \\\"x\\\"\"}"}
                : new Object[] {502, "bad gateway text"});
        Client c = Client.create(url);
        RatelimitdException e = assertThrows(RatelimitdException.class, () -> c.check(CheckRequest.builder("k").policy("x").build()));
        assertEquals(404, e.status());
        assertTrue(e.getMessage().contains("unknown policy \"x\""));
        RatelimitdException e2 = assertThrows(RatelimitdException.class, c::policies);
        assertEquals(502, e2.status());
        assertTrue(e2.getMessage().contains("bad gateway text"));
    }

    @Test
    void resetPoliciesHealthy() throws Exception {
        String url = serve((p, b) -> switch (p) {
            case "/v1/reset" -> new Object[] {204, null};
            case "/v1/policies" -> new Object[] {
                200, "{\"policies\":[{\"name\":\"login\",\"description\":\"d\",\"rules\":[{\"limit\":5,\"window\":\"1m\"}]}]}"
            };
            default -> new Object[] {503, "{\"status\":\"store unavailable\"}"};
        });
        Client c = Client.create(url);
        c.reset(CheckRequest.builder("u").policy("login").build());
        List<Policy> ps = c.policies();
        assertEquals("login", ps.get(0).name());
        assertEquals(5, ps.get(0).rules().get(0).limit());
        assertEquals(503, assertThrows(RatelimitdException.class, c::healthy).status());
    }

    @Test
    void degradedFlagIsDecoded() throws Exception {
        String url = serve((p, b) -> new Object[] {
            200, "{\"allowed\":true,\"policy\":\"p\",\"degraded\":true,\"error\":\"store unavailable\",\"rules\":[]}"
        });
        CheckResult r = Client.create(url).check(CheckRequest.builder("k").policy("p").build());
        assertTrue(r.allowed() && r.degraded());
        assertEquals("store unavailable", r.error());
    }

    @Test
    void checkAsync() throws Exception {
        String url = serve((p, b) -> new Object[] {200, "{\"allowed\":true,\"rules\":[],\"policy\":\"p\"}"});
        assertTrue(Client.create(url).checkAsync(CheckRequest.builder("k").policy("p").build()).get().allowed());
    }

    @Test
    void timeout() throws Exception {
        String url = serve((p, b) -> {
            try {
                Thread.sleep(500);
            } catch (InterruptedException ignored) {
                Thread.currentThread().interrupt();
            }
            return new Object[] {200, "{\"allowed\":true,\"rules\":[]}"};
        });
        Client c = Client.builder(url).timeout(Duration.ofMillis(50)).build();
        assertThrows(IOException.class, () -> c.check(CheckRequest.builder("k").policy("p").build()));
    }
}
