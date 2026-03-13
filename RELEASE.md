# Release Process

This document describes how to cut a new release of
`opentelemetry-go-compile-instrumentation`.

## Release Cadence

Releases are cut approximately every **6 weeks**, following the broader
OpenTelemetry release cadence. Patch releases (hotfixes) are cut as needed
between scheduled releases when a critical bug or security issue is found.

## Release Shepherd

Each release is owned by a **release shepherd** — one of the project
[maintainers](CONTRIBUTING.md#maintainers). The shepherd is responsible for
driving the release end-to-end: tagging, monitoring the workflow, and
publishing the final release notes.

Shepherds rotate among maintainers across releases to share the load.

## Before You Release

1. **Ensure `main` is green.** All required CI workflows must be passing on
   the commit you intend to tag.

2. **Verify there are no pending breaking-change PRs** that should land before
   this release.

3. **Confirm the version number.** This project follows
   [Semantic Versioning](https://semver.org/):
   - `PATCH` (`v0.x.Y+1`) — bug fixes, no API changes
   - `MINOR` (`v0.X+1.0`) — new features, backward-compatible
   - `MAJOR` (`vX+1.0.0`) — breaking changes

4. **Check open issues and PRs** for anything that should be included in this
   release milestone.

## Release Steps

### 1. Create and push the version tag

Tags must follow the pattern `v<MAJOR>.<MINOR>.<PATCH>` (e.g., `v0.3.0`):

```sh
git checkout main
git pull origin main
git tag v0.3.0
git push origin v0.3.0
```

Pushing the tag triggers the [Release workflow](.github/workflows/release.yml)
automatically.

### 2. Monitor the Release workflow

Open the
[Actions tab](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/actions/workflows/release.yml)
and watch the `Build and Release` job. It runs the following steps:

1. Checks out the tagged commit
2. Sets up Go (version from `go.mod`)
3. Runs `make build-all` — cross-compiles `otelc` for all supported platforms
4. Creates a GitHub release and uploads the binaries

### 3. Review and publish the release

The workflow publishes the GitHub release automatically using
`softprops/action-gh-release` with auto-generated release notes. After the
workflow completes:

1. Open the [Releases page](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/releases)
2. Review the auto-generated release notes (derived from PR titles since the
   previous tag).
3. Edit the notes to add context, highlight breaking changes, or group entries
   as needed.
4. Publish the release if it was created as a draft, or confirm it looks
   correct if already published.

## Cross-Compilation Targets

`make build-all` compiles `otelc` for the following platforms:

| OS      | Architecture | Binary name                  |
|---------|-------------|------------------------------|
| Linux   | amd64        | `otelc-linux-amd64`          |
| Linux   | arm64        | `otelc-linux-arm64`          |
| macOS   | amd64        | `otelc-darwin-amd64`         |
| macOS   | arm64        | `otelc-darwin-arm64`         |
| Windows | amd64        | `otelc-windows-amd64.exe`    |

All binaries are uploaded to the GitHub release as downloadable assets.

## Post-Release Verification

After the release is published:

1. **Download a binary** for your platform from the release page and run:
   ```sh
   ./otelc version
   ```
   Confirm it prints the expected version, commit hash, and build date.

2. **Verify the GitHub release** lists all 5 platform binaries.

3. **Announce the release** in the
   [#otel-go-compt-instr-sig](https://cloud-native.slack.com/archives/C088D8GSSSF)
   Slack channel with a link to the release notes.

## Patch Releases (Hotfixes)

For critical bug fixes between scheduled releases:

1. If the fix is on `main`, simply tag from `main` using the next `PATCH`
   version (e.g., `v0.2.1`).

2. If `main` has moved on with unrelated changes that should not be included,
   create a release branch from the previous tag:
   ```sh
   git checkout -b release/v0.2.x v0.2.0
   git cherry-pick <fix-commit-sha>
   git push origin release/v0.2.x
   git tag v0.2.1
   git push origin v0.2.1
   ```

3. Follow the same [release steps](#release-steps) above.

4. If a release branch was created, open a PR to merge the fix back to `main`
   to avoid regression.
