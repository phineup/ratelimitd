// libcurl transport. Compiled once into the library (src/curl_transport.cpp) or, with
// RATELIMITD_HEADER_ONLY, included from <ratelimitd/ratelimitd.hpp>. Not for direct inclusion.
#pragma once

#include <curl/curl.h>

#include <memory>
#include <mutex>
#include <string>
#include <utility>
#include <vector>

#include "ratelimitd/ratelimitd.hpp"

namespace ratelimitd {
namespace detail {

inline void ensure_curl_init() {
    static std::once_flag once;
    std::call_once(once, [] { curl_global_init(CURL_GLOBAL_DEFAULT); });
}

inline std::size_t write_cb(char* data, std::size_t size, std::size_t nmemb, void* userp) {
    static_cast<std::string*>(userp)->append(data, size * nmemb);
    return size * nmemb;
}

class CurlTransport final : public Transport {
public:
    HttpResponse send(const std::string& method, const std::string& url,
                      const std::vector<std::pair<std::string, std::string>>& headers,
                      const std::string& body, std::chrono::milliseconds timeout) override {
        ensure_curl_init();
        std::unique_ptr<CURL, decltype(&curl_easy_cleanup)> h(curl_easy_init(), curl_easy_cleanup);
        if (!h) throw TransportError("ratelimitd: curl_easy_init failed");

        std::string out;
        curl_slist* list = nullptr;
        for (const auto& [k, v] : headers) list = curl_slist_append(list, (k + ": " + v).c_str());
        std::unique_ptr<curl_slist, decltype(&curl_slist_free_all)> list_guard(list, curl_slist_free_all);

        curl_easy_setopt(h.get(), CURLOPT_URL, url.c_str());
        curl_easy_setopt(h.get(), CURLOPT_CUSTOMREQUEST, method.c_str());
        curl_easy_setopt(h.get(), CURLOPT_HTTPHEADER, list);
        if (!body.empty()) {
            curl_easy_setopt(h.get(), CURLOPT_POSTFIELDS, body.c_str());
            curl_easy_setopt(h.get(), CURLOPT_POSTFIELDSIZE_LARGE, static_cast<curl_off_t>(body.size()));
        }
        curl_easy_setopt(h.get(), CURLOPT_WRITEFUNCTION, write_cb);
        curl_easy_setopt(h.get(), CURLOPT_WRITEDATA, &out);
        curl_easy_setopt(h.get(), CURLOPT_TIMEOUT_MS, static_cast<long>(timeout.count()));
        curl_easy_setopt(h.get(), CURLOPT_NOSIGNAL, 1L);

        CURLcode rc = curl_easy_perform(h.get());
        if (rc != CURLE_OK) throw TransportError(std::string("ratelimitd: ") + curl_easy_strerror(rc));

        long status = 0;
        curl_easy_getinfo(h.get(), CURLINFO_RESPONSE_CODE, &status);
        return HttpResponse{static_cast<int>(status), std::move(out)};
    }
};

}  // namespace detail

RATELIMITD_INLINE std::shared_ptr<Transport> make_curl_transport() {
    return std::make_shared<detail::CurlTransport>();
}

}  // namespace ratelimitd
