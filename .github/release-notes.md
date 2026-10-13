## Packages in this release

Everything below is built from tag `@TAG@`. Git tags and the container use the `v` form; language
packages use plain `@VERSION@` because registries normalize or reject a leading `v`.

### Server

| artifact | reference |
|---|---|
| Container | `ghcr.io/@REPO@:@TAG@` (also `:@MAJOR_MINOR@`, `:latest` for stable releases) |
| Binaries | `ratelimitd_@TAG@_<linux\|darwin>_<amd64\|arm64>.tar.gz` attached below, with `checksums.txt` |

### Client SDKs

| SDK | package | version | install |
|---|---|---|---|
| Go | `github.com/phineup/ratelimitd/sdk/go` | `@TAG@` (tag `sdk/go/@TAG@`) | `go get github.com/phineup/ratelimitd/sdk/go@@TAG@` |
| TypeScript / JavaScript | `@phineup/ratelimitd` (GitHub Packages npm) | `@VERSION@` | `npm i @phineup/ratelimitd@@VERSION@` |
| Java | `com.phineup:ratelimitd` (GitHub Packages Maven) | `@VERSION@` | `<version>@VERSION@</version>` |
| C# / .NET | `Phineup.Ratelimitd` (GitHub Packages NuGet) | `@VERSION@` | `dotnet add package Phineup.Ratelimitd --version @VERSION@` |
| Python | `ratelimitd` (git) | tag `@TAG@` | `pip install "git+https://github.com/@REPO@.git@@TAG@#subdirectory=sdk/python"` |
| Rust | `ratelimitd` (git) | tag `@TAG@` | `ratelimitd = { git = "https://github.com/@REPO@", tag = "@TAG@" }` |
| PHP | `phineup/ratelimitd` (Composer VCS) | `@VERSION@` | `composer require phineup/ratelimitd:@VERSION@` |
| Ruby | `ratelimitd` (GitHub Packages RubyGems) | `@VERSION@` | `gem "ratelimitd", "@VERSION@"` from `source "https://rubygems.pkg.github.com/phineup"` |
| C++ | xmake `ratelimitd` / CMake `ratelimitd::cpp` | tag `@TAG@` | `GIT_TAG @TAG@` with `FetchContent`, or the [xmake-repo](https://github.com/phineup/xmake-repo) package |

Per-SDK installation guides: [`sdk/README.md`](https://github.com/@REPO@/blob/@TAG@/sdk/README.md).

---

