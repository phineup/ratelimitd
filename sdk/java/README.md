# ratelimitd (Java)

Java 17+ client for [ratelimitd](https://github.com/phineup/ratelimitd) built on `java.net.http` and Jackson.

```java
Client rl = Client.builder("http://localhost:8080").token("secret").build();
CheckResult res = rl.check(CheckRequest.builder("user-42").policy("login").build());
if (!res.allowed()) {
    // 429 is a normal result; res.retryAfter() is a java.time.Duration
}
```

Methods: `check`, `checkAsync`, `allow`, `reset`, `policies`, `healthy`. Other non-2xx responses throw `RatelimitdException` (`status()`).

```
mvn test
RATELIMITD_URL=http://127.0.0.1:18080 mvn test
```
