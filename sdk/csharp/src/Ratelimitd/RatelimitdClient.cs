using System.Net;
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Text.Json;

namespace Ratelimitd;

/// <summary>Thread-safe client for a ratelimitd server.</summary>
public sealed class RatelimitdClient : IDisposable
{
    private readonly HttpClient _http;
    private readonly bool _ownsHttp;
    private readonly string _baseUrl;

    /// <param name="baseUrl">e.g. "http://localhost:8080"</param>
    /// <param name="token">Bearer token when the server has auth enabled.</param>
    /// <param name="httpClient">Optional shared HttpClient (not disposed by this client).</param>
    public RatelimitdClient(string baseUrl, string? token = null, HttpClient? httpClient = null)
    {
        _baseUrl = baseUrl.TrimEnd('/');
        _ownsHttp = httpClient is null;
        _http = httpClient ?? new HttpClient { Timeout = TimeSpan.FromSeconds(10) };
        if (!string.IsNullOrEmpty(token))
        {
            _http.DefaultRequestHeaders.Authorization = new AuthenticationHeaderValue("Bearer", token);
        }
    }

    /// <summary>Evaluate (and by default consume) a request.</summary>
    public async Task<CheckResult> CheckAsync(CheckRequest request, CancellationToken ct = default)
    {
        using var res = await PostAsync("/v1/check", request, ct).ConfigureAwait(false);
        if (res.StatusCode != HttpStatusCode.OK && res.StatusCode != HttpStatusCode.TooManyRequests)
        {
            throw await ToException(res, ct).ConfigureAwait(false);
        }
        return await res.Content.ReadFromJsonAsync<CheckResult>(ct).ConfigureAwait(false)
               ?? throw new RatelimitdException((int)res.StatusCode, "empty response");
    }

    /// <summary>Shorthand: check <paramref name="key"/> against a named policy with cost 1.</summary>
    public async Task<bool> AllowAsync(string policy, string key, CancellationToken ct = default) =>
        (await CheckAsync(new CheckRequest { Key = key, Policy = policy }, ct).ConfigureAwait(false)).Allowed;

    public async Task ResetAsync(CheckRequest request, CancellationToken ct = default)
    {
        using var res = await PostAsync("/v1/reset", request, ct).ConfigureAwait(false);
        if (res.StatusCode != HttpStatusCode.NoContent && res.StatusCode != HttpStatusCode.OK)
        {
            throw await ToException(res, ct).ConfigureAwait(false);
        }
    }

    public async Task<IReadOnlyList<Policy>> PoliciesAsync(CancellationToken ct = default)
    {
        using var res = await _http.GetAsync(_baseUrl + "/v1/policies", ct).ConfigureAwait(false);
        if (res.StatusCode != HttpStatusCode.OK)
        {
            throw await ToException(res, ct).ConfigureAwait(false);
        }
        var body = await res.Content.ReadFromJsonAsync<PolicyList>(ct).ConfigureAwait(false);
        return body?.Policies ?? new List<Policy>();
    }

    /// <summary>Completes when healthy; throws <see cref="RatelimitdException"/> otherwise.</summary>
    public async Task HealthyAsync(CancellationToken ct = default)
    {
        using var res = await _http.GetAsync(_baseUrl + "/healthz", ct).ConfigureAwait(false);
        if (res.StatusCode != HttpStatusCode.OK)
        {
            throw await ToException(res, ct).ConfigureAwait(false);
        }
    }

    public void Dispose()
    {
        if (_ownsHttp)
        {
            _http.Dispose();
        }
    }

    private sealed record PolicyList(
        [property: System.Text.Json.Serialization.JsonPropertyName("policies")] List<Policy> Policies);

    private Task<HttpResponseMessage> PostAsync(string path, CheckRequest body, CancellationToken ct) =>
        _http.PostAsJsonAsync(_baseUrl + path, body, ct);

    private static async Task<RatelimitdException> ToException(HttpResponseMessage res, CancellationToken ct)
    {
        var text = (await res.Content.ReadAsStringAsync(ct).ConfigureAwait(false)).Trim();
        var msg = text;
        try
        {
            using var doc = JsonDocument.Parse(text);
            if (doc.RootElement.ValueKind == JsonValueKind.Object &&
                doc.RootElement.TryGetProperty("error", out var e) &&
                e.ValueKind == JsonValueKind.String &&
                !string.IsNullOrEmpty(e.GetString()))
            {
                msg = e.GetString()!;
            }
        }
        catch (JsonException)
        {
            // not JSON; keep the raw body
        }
        if (string.IsNullOrEmpty(msg))
        {
            msg = res.ReasonPhrase ?? $"HTTP {(int)res.StatusCode}";
        }
        return new RatelimitdException((int)res.StatusCode, msg);
    }
}
