# ratelimitd — Java SDK

Java 17+ client for [ratelimitd](https://github.com/phineup/ratelimitd) built on `java.net.http` and Jackson (`jackson-databind` is the only dependency).

- Coordinates: `com.phineup:ratelimitd`
- Package: `com.phineup.ratelimitd`

## Install

### Maven — GitHub Packages

Releases are published to GitHub Packages (not Maven Central). GitHub Packages needs credentials even for reads: create a
personal access token with `read:packages` and add it to `~/.m2/settings.xml`:

```xml
<settings>
  <servers>
    <server>
      <id>phineup</id>
      <username>YOUR_GITHUB_USERNAME</username>
      <password>YOUR_TOKEN</password>
    </server>
  </servers>
</settings>
```

`pom.xml`:

```xml
<repositories>
  <repository>
    <id>phineup</id>
    <url>https://maven.pkg.github.com/phineup/ratelimitd</url>
  </repository>
</repositories>

<dependencies>
  <dependency>
    <groupId>com.phineup</groupId>
    <artifactId>ratelimitd</artifactId>
    <version>1.1.0</version>
  </dependency>
</dependencies>
```

### Gradle

```kotlin
repositories {
    maven {
        url = uri("https://maven.pkg.github.com/phineup/ratelimitd")
        credentials {
            username = System.getenv("GITHUB_ACTOR")
            password = System.getenv("GITHUB_TOKEN")
        }
    }
}
dependencies { implementation("com.phineup:ratelimitd:1.1.0") }
```

### From source (no registry, no token)

```bash
git clone https://github.com/phineup/ratelimitd.git
cd ratelimitd/sdk/java && mvn install        # installs into ~/.m2, then depend on it as above
```

## Usage

```java
import com.phineup.ratelimitd.*;
import java.time.Duration;
import java.util.List;

Client rl = Client.builder("http://localhost:8080")
        .token("secret")                       // only when the server has auth enabled
        .timeout(Duration.ofSeconds(2))
        .build();

// Named policy defined in the server config.
CheckResult res = rl.check(CheckRequest.builder("user-42").policy("login").build());
if (!res.allowed()) {
    // 429 is a normal result, not an exception
    System.out.println("limited by " + res.limitedBy() + ", retry in " + res.retryAfter());
}

// Inline rules, no server config needed.
rl.check(CheckRequest.builder("1.2.3.4")
        .rules(List.of(Rule.of("sliding_window", 10, "1m")))
        .build());
rl.check(CheckRequest.builder("u").rules(List.of(Rule.of("token_bucket", 30, "1m").withBurst(5))).build());

// Cost and peek (look without consuming).
rl.check(CheckRequest.builder("u").policy("login").cost(3).peek(true).build());

// Shorthand, async, reset, introspection.
boolean ok = rl.allow("login", "user-42");
rl.checkAsync(CheckRequest.builder("user-42").policy("login").build()).thenAccept(r -> { /* ... */ });
rl.reset(CheckRequest.builder("user-42").policy("login").build());
List<Policy> policies = rl.policies();
rl.healthy();
```

## API

| method | description |
|---|---|
| `Client.builder(url)` / `Client.create(url)` | builder options: `token`, `httpClient`, `timeout` |
| `check(CheckRequest)` / `checkAsync(...)` | returns `CheckResult` / `CompletableFuture<CheckResult>` |
| `allow(policy, key)` | shorthand returning `boolean` |
| `reset(CheckRequest)` | clear counters |
| `policies()` | `List<Policy>` |
| `healthy()` | returns normally when healthy |

`CheckResult` is a record: `allowed`, `consumed`, `remaining`, `retryAfterMs`, `limitedBy`, `rules`, `policy`, `degraded`, `error`,
plus `retryAfter()` as a `Duration`. Non-2xx responses other than 429 throw `RatelimitdException` (`status()`).
Network failures surface as `IOException`.

## Test

```bash
mvn test
RATELIMITD_URL=http://127.0.0.1:18080 mvn test
```
