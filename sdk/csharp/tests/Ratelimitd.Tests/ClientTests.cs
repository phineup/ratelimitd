using System.Net;
using System.Text;
using System.Text.Json;
using Xunit;

namespace Ratelimitd.Tests;

internal sealed class FakeHandler : HttpMessageHandler
{
    private readonly Func<HttpRequestMessage, string, (HttpStatusCode, string?)> _respond;
    public List<(HttpMethod Method, string Path, string? Auth, string? ContentType, string Body)> Seen { get; } = new();

    public FakeHandler(Func<HttpRequestMessage, string, (HttpStatusCode, string?)> respond) => _respond = respond;

    protected override async Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken ct)
    {
        var body = request.Content is null ? "" : await request.Content.ReadAsStringAsync(ct);
        Seen.Add((request.Method, request.RequestUri!.AbsolutePath, request.Headers.Authorization?.ToString(),
            request.Content?.Headers.ContentType?.MediaType, body));
        var (status, payload) = _respond(request, body);
        var res = new HttpResponseMessage(status);
        if (payload is not null)
        {
            res.Content = new StringContent(payload, Encoding.UTF8, "application/json");
        }
        return res;
    }
}

public class ClientTests
{
    private static (RatelimitdClient, FakeHandler) Make(
        Func<HttpRequestMessage, string, (HttpStatusCode, string?)> respond, string? token = null)
    {
        var h = new FakeHandler(respond);
        return (new RatelimitdClient("http://rl.test/", token, new HttpClient(h)), h);
    }

    [Fact]
    public async Task Check_EncodesRequest_AndDecodesAllowed()
    {
        var (c, h) = Make((_, _) => (HttpStatusCode.OK,
            """{"allowed":true,"consumed":true,"remaining":4,"retry_after_ms":0,"policy":"login","rules":[],"future":1}"""), "tok");

        var r = await c.CheckAsync(new CheckRequest { Key = "u", Policy = "login", Cost = 2, Peek = true });

        Assert.True(r.Allowed);
        Assert.Equal(4, r.Remaining);
        var seen = Assert.Single(h.Seen);
        Assert.Equal("/v1/check", seen.Path);
        Assert.Equal("Bearer tok", seen.Auth);
        Assert.Equal("application/json", seen.ContentType);
        using var doc = JsonDocument.Parse(seen.Body);
        var root = doc.RootElement;
        Assert.Equal("u", root.GetProperty("key").GetString());
        Assert.Equal("login", root.GetProperty("policy").GetString());
        Assert.Equal(2, root.GetProperty("cost").GetInt32());
        Assert.True(root.GetProperty("peek").GetBoolean());
        Assert.False(root.TryGetProperty("rules", out _));
    }

    [Fact]
    public async Task Denied_IsAResult_NotAnException()
    {
        var (c, _) = Make((_, _) => (HttpStatusCode.TooManyRequests,
            """{"allowed":false,"consumed":false,"remaining":0,"retry_after_ms":1500,"limited_by":"r","policy":"p","rules":[{"name":"r","algorithm":"fixed_window","allowed":false,"limit":1,"remaining":0,"retry_after_ms":1500,"reset_after_ms":1500}]}"""));

        var r = await c.CheckAsync(new CheckRequest { Key = "u", Policy = "p" });

        Assert.False(r.Allowed);
        Assert.Equal(TimeSpan.FromMilliseconds(1500), r.RetryAfter);
        Assert.Equal("r", r.LimitedBy);
        Assert.Equal(1500, r.Rules[0].RetryAfterMs);
        Assert.False(await c.AllowAsync("p", "u"));
    }

    [Fact]
    public async Task InlineRules_OmitUnsetFields()
    {
        var (c, h) = Make((_, _) => (HttpStatusCode.OK, """{"allowed":true,"rules":[],"policy":"inline"}"""));

        await c.CheckAsync(new CheckRequest
        {
            Key = "1.2.3.4",
            Rules = new() { new Rule { Algorithm = "token_bucket", Limit = 10, Window = "1m", Burst = 20 } },
        });

        using var doc = JsonDocument.Parse(h.Seen[0].Body);
        var rule = doc.RootElement.GetProperty("rules")[0];
        Assert.Equal("token_bucket", rule.GetProperty("algorithm").GetString());
        Assert.Equal(10, rule.GetProperty("limit").GetInt64());
        Assert.Equal(20, rule.GetProperty("burst").GetInt64());
        Assert.False(rule.TryGetProperty("name", out _));
        Assert.False(rule.TryGetProperty("anchor", out _));
        Assert.False(doc.RootElement.TryGetProperty("policy", out _));
    }

    [Fact]
    public async Task ErrorStatuses_BecomeExceptions()
    {
        var (c, _) = Make((req, _) => req.RequestUri!.AbsolutePath == "/v1/check"
            ? (HttpStatusCode.NotFound, """{"error":"unknown policy \"x\""}""")
            : (HttpStatusCode.BadGateway, "bad gateway text"));

        var e = await Assert.ThrowsAsync<RatelimitdException>(() => c.CheckAsync(new CheckRequest { Key = "k", Policy = "x" }));
        Assert.Equal(404, e.StatusCode);
        Assert.Contains("unknown policy \"x\"", e.Message);

        var e2 = await Assert.ThrowsAsync<RatelimitdException>(() => c.PoliciesAsync());
        Assert.Equal(502, e2.StatusCode);
        Assert.Contains("bad gateway text", e2.Message);
    }

    [Fact]
    public async Task Reset_Policies_Healthy()
    {
        var (c, _) = Make((req, _) => req.RequestUri!.AbsolutePath switch
        {
            "/v1/reset" => (HttpStatusCode.NoContent, null),
            "/v1/policies" => (HttpStatusCode.OK, """{"policies":[{"name":"login","description":"d","rules":[{"limit":5,"window":"1m"}]}]}"""),
            _ => (HttpStatusCode.ServiceUnavailable, """{"status":"store unavailable"}"""),
        });

        await c.ResetAsync(new CheckRequest { Key = "u", Policy = "login" });
        var ps = await c.PoliciesAsync();
        Assert.Equal("login", ps[0].Name);
        Assert.Equal(5, ps[0].Rules[0].Limit);
        var e = await Assert.ThrowsAsync<RatelimitdException>(() => c.HealthyAsync());
        Assert.Equal(503, e.StatusCode);
    }

    [Fact]
    public async Task DegradedFlag_IsDecoded()
    {
        var (c, _) = Make((_, _) => (HttpStatusCode.OK,
            """{"allowed":true,"policy":"p","degraded":true,"error":"store unavailable","rules":[]}"""));
        var r = await c.CheckAsync(new CheckRequest { Key = "k", Policy = "p" });
        Assert.True(r.Allowed && r.Degraded);
        Assert.Equal("store unavailable", r.Error);
    }

    [Fact]
    public async Task Cancellation_IsObserved()
    {
        var (c, _) = Make((_, _) => (HttpStatusCode.OK, "{}"));
        using var cts = new CancellationTokenSource();
        cts.Cancel();
        await Assert.ThrowsAnyAsync<OperationCanceledException>(
            () => c.CheckAsync(new CheckRequest { Key = "k", Policy = "p" }, cts.Token));
    }
}
