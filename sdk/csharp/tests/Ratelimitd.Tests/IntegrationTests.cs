using Xunit;

namespace Ratelimitd.Tests;

/// <summary>
/// Real-server tests; they pass trivially unless RATELIMITD_URL is set:
/// <c>go run ./cmd/ratelimitd -config sdk/test-config.json</c>.
/// </summary>
public class IntegrationTests
{
    private static RatelimitdClient? Client()
    {
        var url = Environment.GetEnvironmentVariable("RATELIMITD_URL");
        return string.IsNullOrEmpty(url)
            ? null
            : new RatelimitdClient(url, Environment.GetEnvironmentVariable("RATELIMITD_TOKEN"));
    }

    [Fact]
    public async Task Flow()
    {
        if (Client() is not { } c) return;
        var key = "cs-" + Guid.NewGuid();
        var rules = new List<Rule> { new() { Algorithm = "fixed_window", Limit = 2, Window = "1m" } };

        await c.HealthyAsync();
        for (var i = 0; i < 2; i++)
        {
            var r = await c.CheckAsync(new CheckRequest { Key = key, Rules = rules });
            Assert.True(r.Allowed && r.Consumed);
        }
        var denied = await c.CheckAsync(new CheckRequest { Key = key, Rules = rules });
        Assert.False(denied.Allowed);
        Assert.True(denied.RetryAfterMs > 0);

        await c.ResetAsync(new CheckRequest { Key = key, Rules = rules });
        var peek = await c.CheckAsync(new CheckRequest { Key = key, Rules = rules, Peek = true });
        Assert.True(peek.Allowed);
        Assert.False(peek.Consumed);
    }

    [Fact]
    public async Task ErrorsAndPolicies()
    {
        if (Client() is not { } c) return;
        var rules = new List<Rule> { new() { Algorithm = "fixed_window", Limit = 1, Window = "1m" } };
        var e422 = await Assert.ThrowsAsync<RatelimitdException>(
            () => c.CheckAsync(new CheckRequest { Key = "k", Rules = rules, Cost = 5 }));
        Assert.Equal(422, e422.StatusCode);
        var e404 = await Assert.ThrowsAsync<RatelimitdException>(
            () => c.CheckAsync(new CheckRequest { Key = "k", Policy = "nope" }));
        Assert.Equal(404, e404.StatusCode);
        Assert.Contains(await c.PoliciesAsync(), p => p.Name == "sdk_test");
    }
}
