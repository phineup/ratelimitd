// Client for the ratelimitd rate limiting sidecar.
//
//   ratelimitd::Client client("http://localhost:8080", {.token = "secret"});
//   auto res = client.check(ratelimitd::CheckRequest::for_policy("user-42", "login"));
//   if (!res.allowed) { /* wait res.retry_after() */ }
//
// C++17. Needs libcurl for the default transport; the JSON codec is built in.
//
// Header-only use: define RATELIMITD_HEADER_ONLY (the CMake target ratelimitd::cpp_header_only
// and the xmake package config `headeronly` do this) and link libcurl; no ratelimitd library.
#pragma once

#include <chrono>
#include <cstdint>
#include <memory>
#include <optional>
#include <stdexcept>
#include <string>
#include <utility>
#include <vector>

#ifdef RATELIMITD_HEADER_ONLY
#define RATELIMITD_INLINE inline
#else
#define RATELIMITD_INLINE
#endif

namespace ratelimitd {

/// An inline rule. `window` uses the server syntax: "30s", "5h", "1d12h".
struct Rule {
    std::optional<std::string> name;
    std::optional<std::string> algorithm;  // fixed_window | sliding_window | sliding_log | token_bucket
    std::int64_t limit = 0;
    std::string window;
    std::optional<std::int64_t> burst;
    std::optional<std::string> anchor;  // clock | first_request

    static Rule make(std::string algorithm, std::int64_t limit, std::string window) {
        Rule r;
        r.algorithm = std::move(algorithm);
        r.limit = limit;
        r.window = std::move(window);
        return r;
    }
};

struct Policy {
    std::string name;
    std::string description;
    std::vector<Rule> rules;
    bool dynamic = false;
};

/// Set either `policy` or `rules`.
struct CheckRequest {
    std::string key;
    std::optional<std::string> policy;
    std::vector<Rule> rules;
    std::optional<std::int64_t> cost;  // server default: 1
    bool peek = false;                 // evaluate without consuming

    static CheckRequest for_policy(std::string key, std::string policy) {
        CheckRequest r;
        r.key = std::move(key);
        r.policy = std::move(policy);
        return r;
    }
    static CheckRequest for_rules(std::string key, std::vector<Rule> rules) {
        CheckRequest r;
        r.key = std::move(key);
        r.rules = std::move(rules);
        return r;
    }
};

struct RuleResult {
    std::string name;
    std::string algorithm;
    bool allowed = false;
    std::int64_t limit = 0;
    std::int64_t remaining = 0;
    std::int64_t retry_after_ms = 0;
    std::int64_t reset_after_ms = 0;
};

/// Decision from Client::check. A denial (HTTP 429) is a normal result.
struct CheckResult {
    bool allowed = false;
    bool consumed = false;
    std::int64_t remaining = 0;
    std::int64_t retry_after_ms = 0;
    std::string limited_by;
    std::vector<RuleResult> rules;
    std::string policy;
    bool degraded = false;  // the server's store was down and it failed open
    std::string error;

    std::chrono::milliseconds retry_after() const { return std::chrono::milliseconds(retry_after_ms); }
};

/// Non-2xx response other than 429.
class ApiError : public std::runtime_error {
public:
    ApiError(int status, std::string message)
        : std::runtime_error("ratelimitd: HTTP " + std::to_string(status) + ": " + message),
          status_(status),
          message_(std::move(message)) {}
    int status() const noexcept { return status_; }
    const std::string& message() const noexcept { return message_; }

private:
    int status_;
    std::string message_;
};

/// Connection, timeout or malformed-response failure.
class TransportError : public std::runtime_error {
public:
    using std::runtime_error::runtime_error;
};

struct HttpResponse {
    int status = 0;
    std::string body;
};

/// Pluggable HTTP transport; implement to use another HTTP stack or to mock in tests.
class Transport {
public:
    virtual ~Transport() = default;
    /// `body` is empty for requests without one. Throw TransportError on network failure.
    virtual HttpResponse send(const std::string& method, const std::string& url,
                              const std::vector<std::pair<std::string, std::string>>& headers,
                              const std::string& body, std::chrono::milliseconds timeout) = 0;
};

/// Default transport (libcurl). Thread-safe; one easy handle per request.
std::shared_ptr<Transport> make_curl_transport();

struct ClientOptions {
    std::string token;  // bearer token; empty = none
    std::chrono::milliseconds timeout{10000};
    std::shared_ptr<Transport> transport;  // null = libcurl
};

/// Thread-safe client.
class Client {
public:
    explicit Client(std::string base_url, ClientOptions options = {});

    /// Evaluate (and by default consume) a request.
    CheckResult check(const CheckRequest& request) const;
    /// Shorthand: check `key` against a named policy with cost 1.
    bool allow(const std::string& policy, const std::string& key) const;
    void reset(const CheckRequest& request) const;
    std::vector<Policy> policies() const;
    /// Returns normally when healthy; throws ApiError otherwise.
    void healthy() const;

private:
    HttpResponse send(const std::string& method, const std::string& path, const std::string& body) const;

    std::string base_url_;
    ClientOptions options_;
};

}  // namespace ratelimitd

#ifdef RATELIMITD_HEADER_ONLY
#include "ratelimitd/detail/client.ipp"
#include "ratelimitd/detail/curl_transport.ipp"
#endif
