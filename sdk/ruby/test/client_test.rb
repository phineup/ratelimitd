# frozen_string_literal: true

require "minitest/autorun"
require "securerandom"
require "socket"
require "ratelimitd"

class ClientTest < Minitest::Test
  def client(token: nil, &respond)
    @seen = []
    transport = lambda do |method, url, headers, body, _timeout|
      @seen << { method: method, url: url, headers: headers, body: body }
      respond.call(url, body)
    end
    Ratelimitd::Client.new("http://rl.test/", token: token, transport: transport)
  end

  def test_check_encodes_request_and_decodes_allowed
    c = client(token: "tok") do
      [200, '{"allowed":true,"consumed":true,"remaining":4,"retry_after_ms":0,"policy":"login","rules":[],"future":1}']
    end
    r = c.check(key: "u", policy: "login", cost: 2, peek: true)

    assert r.allowed
    assert r.consumed
    assert_equal 4, r.remaining
    assert_equal "login", r.policy
    s = @seen.first
    assert_equal "POST", s[:method]
    assert_equal "http://rl.test/v1/check", s[:url]
    assert_equal "Bearer tok", s[:headers]["Authorization"]
    assert_equal "application/json", s[:headers]["Content-Type"]
    assert_equal({ "key" => "u", "policy" => "login", "cost" => 2, "peek" => true }, JSON.parse(s[:body]))
  end

  def test_denied_is_a_result_not_an_error
    c = client do
      [429, '{"allowed":false,"consumed":false,"remaining":0,"retry_after_ms":1500,"limited_by":"r","policy":"p","rules":[{"name":"r"}]}']
    end
    r = c.check(key: "u", policy: "p")

    refute r.allowed
    assert_equal 1500, r.retry_after_ms
    assert_in_delta 1.5, r.retry_after
    assert_equal "r", r.limited_by
    assert_equal 1, r.rules.size
    refute c.allow?("p", "u")
  end

  def test_errors
    c = client do |url, _|
      url.end_with?("/v1/check") ? [404, '{"error":"unknown policy \"x\""}'] : [502, "bad gateway text"]
    end
    e = assert_raises(Ratelimitd::ApiError) { c.check(key: "k", policy: "x") }
    assert_equal 404, e.status
    assert_equal 'unknown policy "x"', e.reason_text
    e = assert_raises(Ratelimitd::ApiError) { c.policies }
    assert_equal 502, e.status
    assert_equal "bad gateway text", e.reason_text
  end

  def test_reset_policies_healthy
    c = client do |url, _|
      if url.end_with?("/v1/reset") then [204, ""]
      elsif url.end_with?("/v1/policies") then [200, '{"policies":[{"name":"login","rules":[{"limit":5,"window":"1m"}]}]}']
      else [503, '{"status":"store unavailable"}']
      end
    end
    assert_nil c.reset(key: "u", policy: "login")
    assert_equal "login", c.policies.first["name"]
    assert_equal 503, assert_raises(Ratelimitd::ApiError) { c.healthy }.status
  end

  def test_degraded_and_malformed
    c = client { [200, '{"allowed":true,"policy":"p","degraded":true,"error":"store unavailable","rules":[]}'] }
    r = c.check(key: "k", policy: "p")
    assert r.allowed && r.degraded
    assert_equal "store unavailable", r.error

    bad = client { [200, "not json"] }
    assert_raises(Ratelimitd::TransportError) { bad.check(key: "k", policy: "p") }
  end

  def test_net_http_transport_against_a_local_socket
    server = TCPServer.new("127.0.0.1", 0)
    port = server.addr[1]
    seen = Queue.new
    t = Thread.new do
      sock = server.accept
      head = +""
      head << sock.readpartial(4096) until head.include?("\r\n\r\n")
      len = head[/content-length: (\d+)/i, 1].to_i
      body = head.split("\r\n\r\n", 2)[1]
      body << sock.readpartial(4096) while body.bytesize < len
      seen << [head.lines.first.strip, body]
      payload = '{"allowed":true,"consumed":true,"rules":[],"policy":"p"}'
      sock.write("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: #{payload.bytesize}\r\nConnection: close\r\n\r\n#{payload}")
      sock.close
    end
    c = Ratelimitd::Client.new("http://127.0.0.1:#{port}", timeout: 2)
    assert c.check(key: "k", policy: "p").allowed
    line, body = seen.pop
    assert_equal "POST /v1/check HTTP/1.1", line
    assert_equal({ "key" => "k", "policy" => "p" }, JSON.parse(body))
    t.join
  ensure
    server&.close
  end

  def test_connection_failure_is_a_transport_error
    s = TCPServer.new("127.0.0.1", 0)
    port = s.addr[1]
    s.close
    c = Ratelimitd::Client.new("http://127.0.0.1:#{port}", timeout: 1)
    assert_raises(Ratelimitd::TransportError) { c.healthy }
  end

  def test_integration
    url = ENV["RATELIMITD_URL"]
    skip "RATELIMITD_URL not set" if url.nil? || url.empty?

    c = Ratelimitd::Client.new(url, token: ENV["RATELIMITD_TOKEN"])
    key = "rb-#{SecureRandom.hex(8)}"
    rules = [{ algorithm: "fixed_window", limit: 2, window: "1m" }]

    c.healthy
    2.times do
      r = c.check(key: key, rules: rules)
      assert r.allowed && r.consumed
    end
    denied = c.check(key: key, rules: rules)
    refute denied.allowed
    assert_operator denied.retry_after_ms, :>, 0

    c.reset(key: key, rules: rules)
    peek = c.check(key: key, rules: rules, peek: true)
    assert peek.allowed
    refute peek.consumed

    assert_equal 422, assert_raises(Ratelimitd::ApiError) { c.check(key: "k", rules: rules, cost: 5) }.status
    assert_equal 404, assert_raises(Ratelimitd::ApiError) { c.check(key: "k", policy: "nope") }.status
    assert_includes c.policies.map { |p| p["name"] }, "sdk_test"
  end
end
