using System.Text.Json.Serialization;

namespace Ratelimitd;

/// <summary>An inline rule. <c>Window</c> uses the server syntax: "30s", "5h", "1d12h".</summary>
public sealed record Rule
{
    [JsonPropertyName("name"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public string? Name { get; init; }

    [JsonPropertyName("algorithm"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public string? Algorithm { get; init; }

    [JsonPropertyName("limit")]
    public long Limit { get; init; }

    [JsonPropertyName("window")]
    public string Window { get; init; } = "";

    [JsonPropertyName("burst"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public long? Burst { get; init; }

    [JsonPropertyName("anchor"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public string? Anchor { get; init; }
}

public sealed record Policy
{
    [JsonPropertyName("name")] public string Name { get; init; } = "";
    [JsonPropertyName("description")] public string? Description { get; init; }
    [JsonPropertyName("rules")] public List<Rule> Rules { get; init; } = new();
    [JsonPropertyName("dynamic")] public bool Dynamic { get; init; }
}

/// <summary>Set either <see cref="Policy"/> or <see cref="Rules"/>.</summary>
public sealed record CheckRequest
{
    [JsonPropertyName("key")]
    public string Key { get; init; } = "";

    [JsonPropertyName("policy"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public string? Policy { get; init; }

    [JsonPropertyName("rules"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public List<Rule>? Rules { get; init; }

    /// <summary>Defaults to 1 on the server when unset.</summary>
    [JsonPropertyName("cost"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public long? Cost { get; init; }

    /// <summary>Evaluate without consuming quota.</summary>
    [JsonPropertyName("peek"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingDefault)]
    public bool Peek { get; init; }
}

public sealed record RuleResult
{
    [JsonPropertyName("name")] public string Name { get; init; } = "";
    [JsonPropertyName("algorithm")] public string Algorithm { get; init; } = "";
    [JsonPropertyName("allowed")] public bool Allowed { get; init; }
    [JsonPropertyName("limit")] public long Limit { get; init; }
    [JsonPropertyName("remaining")] public long Remaining { get; init; }
    [JsonPropertyName("retry_after_ms")] public long RetryAfterMs { get; init; }
    [JsonPropertyName("reset_after_ms")] public long ResetAfterMs { get; init; }
}

/// <summary>Decision from <see cref="RatelimitdClient.CheckAsync"/>. A denial (HTTP 429) is a normal result.</summary>
public sealed record CheckResult
{
    [JsonPropertyName("allowed")] public bool Allowed { get; init; }
    [JsonPropertyName("consumed")] public bool Consumed { get; init; }
    [JsonPropertyName("remaining")] public long Remaining { get; init; }
    [JsonPropertyName("retry_after_ms")] public long RetryAfterMs { get; init; }
    [JsonPropertyName("limited_by")] public string? LimitedBy { get; init; }
    [JsonPropertyName("rules")] public List<RuleResult> Rules { get; init; } = new();
    [JsonPropertyName("policy")] public string Policy { get; init; } = "";

    /// <summary>True when the server's store was down and it failed open.</summary>
    [JsonPropertyName("degraded")] public bool Degraded { get; init; }

    [JsonPropertyName("error")] public string? Error { get; init; }

    [JsonIgnore] public TimeSpan RetryAfter => TimeSpan.FromMilliseconds(RetryAfterMs);
}

/// <summary>Non-2xx response other than 429.</summary>
public sealed class RatelimitdException : Exception
{
    public int StatusCode { get; }

    public RatelimitdException(int statusCode, string message)
        : base($"ratelimitd: HTTP {statusCode}: {message}") => StatusCode = statusCode;
}
