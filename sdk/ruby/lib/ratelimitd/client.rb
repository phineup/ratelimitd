# frozen_string_literal: true

require "json"
require "net/http"
require "uri"

module Ratelimitd
  # Non-2xx response other than 429 (a denial is a normal CheckResult).
  class ApiError < StandardError
    attr_reader :status, :reason_text

    def initialize(status, reason_text)
      @status = status
      @reason_text = reason_text
      super("ratelimitd: HTTP #{status}: #{reason_text}")
    end
  end

  # Connection, timeout or malformed-response failure.
  class TransportError < StandardError; end

  CheckResult = Struct.new(
    :allowed, :consumed, :remaining, :retry_after_ms, :limited_by, :rules, :policy, :degraded, :error,
    keyword_init: true
  ) do
    def retry_after
      retry_after_ms / 1000.0
    end

    def self.from_hash(h)
      new(
        allowed: h.fetch("allowed", false),
        consumed: h.fetch("consumed", false),
        remaining: h.fetch("remaining", 0),
        retry_after_ms: h.fetch("retry_after_ms", 0),
        limited_by: h["limited_by"],
        rules: h.fetch("rules", nil) || [],
        policy: h.fetch("policy", ""),
        degraded: h.fetch("degraded", false),
        error: h["error"]
      )
    end
  end

  # Thread-safe client. Requests are plain hashes so every server field is available:
  #
  #   client.check(key: "user-42", policy: "login")
  #   client.check(key: "1.2.3.4", rules: [{ algorithm: "sliding_window", limit: 10, window: "1m" }])
  class Client
    # transport: optional callable (method, url, headers, body, timeout) => [status, body]
    def initialize(base_url, token: nil, timeout: 10.0, transport: nil)
      @base_url = base_url.sub(%r{/+\z}, "")
      @token = token
      @timeout = timeout
      @transport = transport
    end

    # Evaluate (and by default consume) a request.
    def check(request)
      status, body = send_request("POST", "/v1/check", request)
      raise error(status, body) unless [200, 429].include?(status)

      CheckResult.from_hash(decode(body))
    end

    # Shorthand: check +key+ against a named policy with cost 1.
    def allow?(policy, key)
      check(key: key, policy: policy).allowed
    end

    def reset(request)
      status, body = send_request("POST", "/v1/reset", request)
      raise error(status, body) unless [200, 204].include?(status)

      nil
    end

    def policies
      status, body = send_request("GET", "/v1/policies", nil)
      raise error(status, body) unless status == 200

      decode(body)["policies"] || []
    end

    # Returns nil when healthy, raises ApiError otherwise.
    def healthy
      status, body = send_request("GET", "/healthz", nil)
      raise error(status, body) unless status == 200

      nil
    end

    private

    def send_request(method, path, payload)
      headers = {}
      body = nil
      unless payload.nil?
        body = JSON.generate(payload)
        headers["Content-Type"] = "application/json"
      end
      headers["Authorization"] = "Bearer #{@token}" if @token && !@token.empty?
      url = @base_url + path
      return @transport.call(method, url, headers, body, @timeout) if @transport

      http_request(method, url, headers, body)
    end

    def http_request(method, url, headers, body)
      uri = URI.parse(url)
      http = Net::HTTP.new(uri.host, uri.port)
      http.use_ssl = uri.scheme == "https"
      http.open_timeout = http.read_timeout = http.write_timeout = @timeout
      req = Net::HTTPGenericRequest.new(method, !body.nil?, true, uri.request_uri, headers)
      res = http.request(req, body)
      [res.code.to_i, res.body.to_s]
    rescue SystemCallError, SocketError, Timeout::Error, IOError, OpenSSL::SSL::SSLError => e
      raise TransportError, "ratelimitd: #{e.message}"
    end

    def decode(body)
      parsed = JSON.parse(body)
      raise TransportError, "ratelimitd: decode: expected a JSON object" unless parsed.is_a?(Hash)

      parsed
    rescue JSON::ParserError => e
      raise TransportError, "ratelimitd: decode: #{e.message}"
    end

    def error(status, body)
      msg = body.to_s.strip
      begin
        parsed = JSON.parse(body)
        msg = parsed["error"] if parsed.is_a?(Hash) && parsed["error"].is_a?(String) && !parsed["error"].empty?
      rescue JSON::ParserError
        # not JSON; keep the raw body
      end
      msg = "HTTP #{status}" if msg.empty?
      ApiError.new(status, msg)
    end
  end
end
