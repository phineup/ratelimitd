# Contributing to ratelimitd

Thanks for your interest! This project is MIT licensed and welcomes contributions.

## Ground rules

- **Zero dependencies.** The service uses the Go standard library only. Pull
  requests that add third-party modules will not be accepted.
- **Memory and Redis stores must stay equivalent.** If you change an algorithm
  in `limiter/algorithms.go`, make the identical change in
  `limiter/redis_script.go`; `TestRedisMatchesMemory` enforces this.
- **Tests required.** Bug fixes need a regression test; features need tests.
- Run before pushing:

  ```bash
  gofmt -l .        # must print nothing
  go vet ./...
  go test -race ./...
  ```

## Developer Certificate of Origin (DCO)

By contributing you certify that you wrote the code or otherwise have the right
to submit it under the project's MIT license, per the
[Developer Certificate of Origin 1.1](https://developercertificate.org/).
Every commit must carry a sign-off line:

```
Signed-off-by: Your Name <you@example.com>
```

Add it automatically with `git commit -s`. Pull requests with unsigned commits
are blocked by CI. Fix them with `git rebase --signoff development` and force-push.

Do not submit code copied from projects with incompatible licenses (e.g. GPL,
AGPL) or code you are not authorized to share (e.g. from your employer).

## Branching model

| branch | purpose |
|---|---|
| `master` | latest stable code; every release tag is cut from here |
| `development` | integration branch; **all contributor PRs target this** |
| `feature/*`, `fix/*`, ... | short-lived branches, in your fork only |

- Work in your own fork (or locally). Branches cannot be created in this repository by non-maintainers.
- Open pull requests against `development`. PRs against `master` are only accepted from `development`.
- Only maintainers can merge to `development` and `master`; both require passing CI and approving reviews
  (`master` requires more reviewers than `development`).
- Version tags (`vX.Y.Z`) are created by maintainers from `master`. A tag triggers the release workflow, which
  builds Linux/macOS binaries for the GitHub release and publishes the container image to
  `ghcr.io/phineup/ratelimitd` with the same `v`-prefixed tag, marking it `latest` (stable tags only;
  `-rc.1` style tags are prereleases).
- Push the Go module tag with every release so `go get github.com/phineup/ratelimitd/sdk/go@vX.Y.Z` resolves
  (the Go SDK lives in a subdirectory, and tag creation is admin-only so CI cannot do it):

  ```bash
  git tag -a vX.Y.Z -m vX.Y.Z && git tag -a sdk/go/vX.Y.Z vX.Y.Z^{} -m "sdk/go vX.Y.Z"
  git push origin vX.Y.Z sdk/go/vX.Y.Z
  ```

## Pull request process

1. Open an issue first for large changes or new algorithms.
2. Fork, create a topic branch from `development`, keep PRs focused and small.
3. Fill in the PR template and make sure CI is green.
4. A maintainer review (see `CODEOWNERS`) is required before merge.

## Security

Do not open public issues for vulnerabilities. See [SECURITY.md](SECURITY.md).

## Conduct

Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md).
