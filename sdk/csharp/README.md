# Phineup.Ratelimitd (.NET)

.NET 8+ client for [ratelimitd](https://github.com/phineup/ratelimitd) using `HttpClient` and `System.Text.Json` only.

```csharp
using var rl = new RatelimitdClient("http://localhost:8080", token: "secret");
var res = await rl.CheckAsync(new CheckRequest { Key = "user-42", Policy = "login" });
if (!res.Allowed)
{
    // 429 is a normal result; res.RetryAfter is a TimeSpan
}
```

Methods: `CheckAsync`, `AllowAsync`, `ResetAsync`, `PoliciesAsync`, `HealthyAsync`. Other non-2xx responses throw `RatelimitdException` (`StatusCode`).

```
dotnet test tests/Ratelimitd.Tests
RATELIMITD_URL=http://127.0.0.1:18080 dotnet test tests/Ratelimitd.Tests
```
