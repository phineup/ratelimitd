# Phineup.Ratelimitd — .NET SDK

.NET 8+ client for [ratelimitd](https://github.com/phineup/ratelimitd) using only `HttpClient` and `System.Text.Json` (no third-party packages).

- NuGet package id: `Phineup.Ratelimitd`
- Namespace: `Ratelimitd`

## Install

### NuGet — GitHub Packages

Releases are published to GitHub Packages (not nuget.org). Create a personal access token with `read:packages`, then add the feed:

```bash
dotnet nuget add source "https://nuget.pkg.github.com/phineup/index.json" \
  --name phineup --username YOUR_GITHUB_USERNAME --password YOUR_TOKEN --store-password-in-clear-text

dotnet add package Phineup.Ratelimitd
```

Or commit a `nuget.config` next to your solution (token via an environment variable):

```xml
<configuration>
  <packageSources>
    <add key="phineup" value="https://nuget.pkg.github.com/phineup/index.json" />
  </packageSources>
  <packageSourceCredentials>
    <phineup>
      <add key="Username" value="YOUR_GITHUB_USERNAME" />
      <add key="ClearTextPassword" value="%GITHUB_TOKEN%" />
    </phineup>
  </packageSourceCredentials>
</configuration>
```

### From source (no registry, no token)

```bash
git clone https://github.com/phineup/ratelimitd.git
dotnet add YourApp.csproj reference ratelimitd/sdk/csharp/src/Ratelimitd/Ratelimitd.csproj
# or build a local package:
dotnet pack ratelimitd/sdk/csharp/src/Ratelimitd -c Release -o ./local-feed
dotnet add package Phineup.Ratelimitd --source ./local-feed
```

## Usage

```csharp
using Ratelimitd;

using var rl = new RatelimitdClient("http://localhost:8080", token: "secret"); // token only if auth is enabled

// Named policy defined in the server config.
var res = await rl.CheckAsync(new CheckRequest { Key = "user-42", Policy = "login" });
if (!res.Allowed)
{
    // 429 is a normal result, not an exception
    Console.WriteLine($"limited by {res.LimitedBy}, retry in {res.RetryAfter}");
}

// Inline rules, no server config needed.
await rl.CheckAsync(new CheckRequest
{
    Key = "1.2.3.4",
    Rules = new() { new Rule { Algorithm = "sliding_window", Limit = 10, Window = "1m" } },
});

// Cost and peek (look without consuming).
await rl.CheckAsync(new CheckRequest { Key = "u", Policy = "login", Cost = 3, Peek = true });

// Shorthand, reset, introspection.
bool ok = await rl.AllowAsync("login", "user-42");
await rl.ResetAsync(new CheckRequest { Key = "user-42", Policy = "login" });
var policies = await rl.PoliciesAsync();
await rl.HealthyAsync();
```

### ASP.NET Core with DI

```csharp
builder.Services.AddHttpClient<RatelimitdClient>(); // or register a singleton with your own HttpClient
builder.Services.AddSingleton(sp =>
    new RatelimitdClient("http://ratelimitd:8080", token: builder.Configuration["Ratelimitd:Token"],
                         httpClient: sp.GetRequiredService<IHttpClientFactory>().CreateClient()));
```

## API

| method | description |
|---|---|
| `new RatelimitdClient(baseUrl, token?, httpClient?)` | pass your own `HttpClient` to control timeouts / handlers |
| `CheckAsync(CheckRequest, ct)` | evaluate and (unless `Peek`) consume |
| `AllowAsync(policy, key, ct)` | shorthand returning `bool` |
| `ResetAsync(CheckRequest, ct)` | clear counters |
| `PoliciesAsync(ct)` | `IReadOnlyList<Policy>` |
| `HealthyAsync(ct)` | completes when healthy |

`CheckResult`: `Allowed`, `Consumed`, `Remaining`, `RetryAfterMs`, `RetryAfter` (`TimeSpan`), `LimitedBy`, `Rules`, `Policy`,
`Degraded`, `Error`. Non-2xx responses other than 429 throw `RatelimitdException` (`StatusCode`).

## Test

```bash
dotnet test tests/Ratelimitd.Tests
RATELIMITD_URL=http://127.0.0.1:18080 dotnet test tests/Ratelimitd.Tests
```
