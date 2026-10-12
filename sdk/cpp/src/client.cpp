#include "ratelimitd/ratelimitd.hpp"

#include "json.hpp"

namespace ratelimitd {
namespace {

json::Value rule_to_json(const Rule& r) {
    json::Object o;
    if (r.name) o["name"] = *r.name;
    if (r.algorithm) o["algorithm"] = *r.algorithm;
    o["limit"] = r.limit;
    o["window"] = r.window;
    if (r.burst) o["burst"] = *r.burst;
    if (r.anchor) o["anchor"] = *r.anchor;
    return o;
}

Rule rule_from_json(const json::Value& v) {
    Rule r;
    if (auto* n = v.find("name"); n && n->is_string()) r.name = n->string();
    if (auto* a = v.find("algorithm"); a && a->is_string()) r.algorithm = a->string();
    r.limit = v.get_int("limit");
    r.window = v.get_string("window");
    if (auto* b = v.find("burst"); b && b->is_number()) r.burst = b->integer();
    if (auto* a = v.find("anchor"); a && a->is_string()) r.anchor = a->string();
    return r;
}

std::string encode(const CheckRequest& req) {
    json::Object o;
    o["key"] = req.key;
    if (req.policy && !req.policy->empty()) o["policy"] = *req.policy;
    if (!req.rules.empty()) {
        json::Array rules;
        for (const auto& r : req.rules) rules.push_back(rule_to_json(r));
        o["rules"] = std::move(rules);
    }
    if (req.cost) o["cost"] = *req.cost;
    if (req.peek) o["peek"] = true;
    return json::Value(std::move(o)).dump();
}

json::Value parse_body(const std::string& body) {
    try {
        return json::Value::parse(body);
    } catch (const std::exception& e) {
        throw TransportError(std::string("ratelimitd: decode: ") + e.what());
    }
}

ApiError api_error(const HttpResponse& res) {
    std::string msg = res.body;
    while (!msg.empty() && (msg.back() == '\n' || msg.back() == '\r' || msg.back() == ' ')) msg.pop_back();
    try {
        auto v = json::Value::parse(res.body);
        auto e = v.get_string("error");
        if (!e.empty()) msg = e;
    } catch (const std::exception&) {
        // not JSON; keep the raw body
    }
    if (msg.empty()) msg = "HTTP " + std::to_string(res.status);
    return ApiError(res.status, msg);
}

}  // namespace

Client::Client(std::string base_url, ClientOptions options)
    : base_url_(std::move(base_url)), options_(std::move(options)) {
    while (!base_url_.empty() && base_url_.back() == '/') base_url_.pop_back();
    if (!options_.transport) options_.transport = make_curl_transport();
}

HttpResponse Client::send(const std::string& method, const std::string& path, const std::string& body) const {
    std::vector<std::pair<std::string, std::string>> headers;
    if (!body.empty()) headers.emplace_back("Content-Type", "application/json");
    if (!options_.token.empty()) headers.emplace_back("Authorization", "Bearer " + options_.token);
    return options_.transport->send(method, base_url_ + path, headers, body, options_.timeout);
}

CheckResult Client::check(const CheckRequest& request) const {
    auto res = send("POST", "/v1/check", encode(request));
    if (res.status != 200 && res.status != 429) throw api_error(res);
    auto v = parse_body(res.body);

    CheckResult out;
    out.allowed = v.get_bool("allowed");
    out.consumed = v.get_bool("consumed");
    out.remaining = v.get_int("remaining");
    out.retry_after_ms = v.get_int("retry_after_ms");
    out.limited_by = v.get_string("limited_by");
    out.policy = v.get_string("policy");
    out.degraded = v.get_bool("degraded");
    out.error = v.get_string("error");
    if (auto* rules = v.find("rules"); rules && rules->is_array()) {
        for (const auto& r : rules->array()) {
            RuleResult rr;
            rr.name = r.get_string("name");
            rr.algorithm = r.get_string("algorithm");
            rr.allowed = r.get_bool("allowed");
            rr.limit = r.get_int("limit");
            rr.remaining = r.get_int("remaining");
            rr.retry_after_ms = r.get_int("retry_after_ms");
            rr.reset_after_ms = r.get_int("reset_after_ms");
            out.rules.push_back(std::move(rr));
        }
    }
    return out;
}

bool Client::allow(const std::string& policy, const std::string& key) const {
    return check(CheckRequest::for_policy(key, policy)).allowed;
}

void Client::reset(const CheckRequest& request) const {
    auto res = send("POST", "/v1/reset", encode(request));
    if (res.status != 204 && res.status != 200) throw api_error(res);
}

std::vector<Policy> Client::policies() const {
    auto res = send("GET", "/v1/policies", "");
    if (res.status != 200) throw api_error(res);
    auto v = parse_body(res.body);
    std::vector<Policy> out;
    if (auto* list = v.find("policies"); list && list->is_array()) {
        for (const auto& p : list->array()) {
            Policy pol;
            pol.name = p.get_string("name");
            pol.description = p.get_string("description");
            pol.dynamic = p.get_bool("dynamic");
            if (auto* rules = p.find("rules"); rules && rules->is_array()) {
                for (const auto& r : rules->array()) pol.rules.push_back(rule_from_json(r));
            }
            out.push_back(std::move(pol));
        }
    }
    return out;
}

void Client::healthy() const {
    auto res = send("GET", "/healthz", "");
    if (res.status != 200) throw api_error(res);
}

}  // namespace ratelimitd
