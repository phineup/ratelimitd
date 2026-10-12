#include <cstdio>
#include <cstdlib>
#include <functional>
#include <string>

#include "json.hpp"
#include "ratelimitd/ratelimitd.hpp"

using namespace ratelimitd;

static int failures = 0;
#define CHECK(cond)                                                          \
    do {                                                                     \
        if (!(cond)) {                                                       \
            std::fprintf(stderr, "%s:%d: CHECK failed: %s\n", __FILE__, __LINE__, #cond); \
            ++failures;                                                      \
        }                                                                    \
    } while (0)

struct Seen {
    std::string method, url, body;
    std::vector<std::pair<std::string, std::string>> headers;
    std::string header(const std::string& k) const {
        for (auto& [hk, hv] : headers)
            if (hk == k) return hv;
        return "";
    }
};

class FakeTransport : public Transport {
public:
    std::function<HttpResponse(const Seen&)> respond;
    std::vector<Seen> seen;
    HttpResponse send(const std::string& method, const std::string& url,
                      const std::vector<std::pair<std::string, std::string>>& headers,
                      const std::string& body, std::chrono::milliseconds) override {
        seen.push_back({method, url, body, headers});
        return respond(seen.back());
    }
};

static std::pair<Client, std::shared_ptr<FakeTransport>> make(std::function<HttpResponse(const Seen&)> r,
                                                              std::string token = "") {
    auto t = std::make_shared<FakeTransport>();
    t->respond = std::move(r);
    ClientOptions o;
    o.token = std::move(token);
    o.transport = t;
    return {Client("http://rl.test/", o), t};
}

template <class F>
static int expect_api_error(F&& f) {
    try {
        f();
    } catch (const ApiError& e) {
        return e.status();
    } catch (...) {
        return -1;
    }
    return 0;
}

static void test_check_encoding_and_decoding() {
    auto [c, t] = make(
        [](const Seen&) {
            return HttpResponse{200, R"({"allowed":true,"consumed":true,"remaining":4,"retry_after_ms":0,"policy":"login","rules":[],"future":1})"};
        },
        "tok");
    auto req = CheckRequest::for_policy("u", "login");
    req.cost = 2;
    req.peek = true;
    auto r = c.check(req);
    CHECK(r.allowed && r.consumed);
    CHECK(r.remaining == 4);
    CHECK(r.policy == "login");

    const auto& s = t->seen.at(0);
    CHECK(s.method == "POST");
    CHECK(s.url == "http://rl.test/v1/check");
    CHECK(s.header("Authorization") == "Bearer tok");
    CHECK(s.header("Content-Type") == "application/json");
    auto body = json::Value::parse(s.body);
    CHECK(body.get_string("key") == "u");
    CHECK(body.get_string("policy") == "login");
    CHECK(body.get_int("cost") == 2);
    CHECK(body.get_bool("peek"));
    CHECK(body.find("rules") == nullptr);
}

static void test_denied_is_a_result() {
    auto [c, t] = make([](const Seen&) {
        return HttpResponse{429, R"({"allowed":false,"consumed":false,"remaining":0,"retry_after_ms":1500,"limited_by":"r","policy":"p","rules":[{"name":"r","algorithm":"fixed_window","allowed":false,"limit":1,"remaining":0,"retry_after_ms":1500,"reset_after_ms":1500}]})"};
    });
    auto r = c.check(CheckRequest::for_policy("u", "p"));
    CHECK(!r.allowed);
    CHECK(r.retry_after() == std::chrono::milliseconds(1500));
    CHECK(r.limited_by == "r");
    CHECK(r.rules.size() == 1 && r.rules[0].retry_after_ms == 1500);
    CHECK(!c.allow("p", "u"));
}

static void test_inline_rules() {
    auto [c, t] = make([](const Seen&) { return HttpResponse{200, R"({"allowed":true,"rules":[],"policy":"inline"})"}; });
    auto rule = Rule::make("token_bucket", 10, "1m");
    rule.burst = 20;
    c.check(CheckRequest::for_rules("1.2.3.4", {rule}));
    auto body = json::Value::parse(t->seen.at(0).body);
    const auto& r0 = body.find("rules")->array().at(0);
    CHECK(r0.get_string("algorithm") == "token_bucket");
    CHECK(r0.get_int("limit") == 10);
    CHECK(r0.get_int("burst") == 20);
    CHECK(r0.find("name") == nullptr);
    CHECK(r0.find("anchor") == nullptr);
    CHECK(body.find("policy") == nullptr);
    CHECK(body.find("cost") == nullptr);
}

static void test_errors() {
    auto cp = make([](const Seen& s) {
        if (s.url.find("/v1/check") != std::string::npos)
            return HttpResponse{404, R"({"error":"unknown policy \"x\""})"};
        if (s.url.find("/v1/policies") != std::string::npos) return HttpResponse{502, "bad gateway text\n"};
        return HttpResponse{503, ""};
    });
    auto& c = cp.first;
    CHECK(expect_api_error([&] { c.check(CheckRequest::for_policy("k", "x")); }) == 404);
    try {
        c.check(CheckRequest::for_policy("k", "x"));
    } catch (const ApiError& e) {
        CHECK(e.message() == "unknown policy \"x\"");
    }
    try {
        c.policies();
        CHECK(false);
    } catch (const ApiError& e) {
        CHECK(e.status() == 502 && e.message() == "bad gateway text");
    }
    try {
        c.healthy();
        CHECK(false);
    } catch (const ApiError& e) {
        CHECK(e.status() == 503 && e.message() == "HTTP 503");
    }
}

static void test_reset_policies_healthy() {
    auto [c, t] = make([](const Seen& s) {
        if (s.url.find("/v1/reset") != std::string::npos) return HttpResponse{204, ""};
        if (s.url.find("/v1/policies") != std::string::npos)
            return HttpResponse{200, R"({"policies":[{"name":"login","description":"d","rules":[{"limit":5,"window":"1m"}]}]})"};
        return HttpResponse{200, R"({"status":"ok"})"};
    });
    c.reset(CheckRequest::for_policy("u", "login"));
    auto ps = c.policies();
    CHECK(ps.size() == 1 && ps[0].name == "login");
    CHECK(ps[0].rules.at(0).limit == 5 && ps[0].rules[0].window == "1m");
    c.healthy();
    CHECK(t->seen.at(0).method == "POST");
    CHECK(t->seen.at(1).method == "GET");
    CHECK(t->seen.at(1).header("Content-Type").empty());
}

static void test_degraded_and_malformed() {
    auto [c, t] = make([](const Seen&) {
        return HttpResponse{200, R"({"allowed":true,"policy":"p","degraded":true,"error":"store unavailable","rules":[]})"};
    });
    auto r = c.check(CheckRequest::for_policy("k", "p"));
    CHECK(r.allowed && r.degraded && r.error == "store unavailable");

    auto [c2, t2] = make([](const Seen&) { return HttpResponse{200, "not json"}; });
    bool threw = false;
    try {
        c2.check(CheckRequest::for_policy("k", "p"));
    } catch (const TransportError&) {
        threw = true;
    }
    CHECK(threw);
}

static void test_transport_error_propagates() {
    auto [c, t] = make([](const Seen&) -> HttpResponse { throw TransportError("boom"); });
    bool threw = false;
    try {
        c.healthy();
    } catch (const TransportError& e) {
        threw = std::string(e.what()) == "boom";
    }
    CHECK(threw);
}

// Real server through libcurl; only when RATELIMITD_URL is set.
static void test_integration() {
    const char* url = std::getenv("RATELIMITD_URL");
    if (!url || !*url) return;
    ClientOptions o;
    if (const char* tok = std::getenv("RATELIMITD_TOKEN")) o.token = tok;
    Client c(url, o);

    std::string key = "cpp-" + std::to_string(std::chrono::steady_clock::now().time_since_epoch().count());
    std::vector<Rule> rules = {Rule::make("fixed_window", 2, "1m")};

    c.healthy();
    for (int i = 0; i < 2; ++i) {
        auto r = c.check(CheckRequest::for_rules(key, rules));
        CHECK(r.allowed && r.consumed);
    }
    auto denied = c.check(CheckRequest::for_rules(key, rules));
    CHECK(!denied.allowed && denied.retry_after_ms > 0);

    c.reset(CheckRequest::for_rules(key, rules));
    auto peek = CheckRequest::for_rules(key, rules);
    peek.peek = true;
    auto pr = c.check(peek);
    CHECK(pr.allowed && !pr.consumed);

    auto bad = CheckRequest::for_rules("k", {Rule::make("fixed_window", 1, "1m")});
    bad.cost = 5;
    CHECK(expect_api_error([&] { c.check(bad); }) == 422);
    CHECK(expect_api_error([&] { c.check(CheckRequest::for_policy("k", "nope")); }) == 404);

    bool found = false;
    for (auto& p : c.policies()) found = found || p.name == "sdk_test";
    CHECK(found);
}

int main() {
    test_check_encoding_and_decoding();
    test_denied_is_a_result();
    test_inline_rules();
    test_errors();
    test_reset_policies_healthy();
    test_degraded_and_malformed();
    test_transport_error_propagates();
    test_integration();

    if (failures) {
        std::fprintf(stderr, "%d failure(s)\n", failures);
        return 1;
    }
    std::puts("test_client: ok");
    return 0;
}
