# Release Process

This document describes how to cut a new release of
`opentelemetry-go-compile-instrumentation`.

## Release Cadence

Releases are cut approximately every **4 weeks** (monthly). Patch releases
(hotfixes) are cut as needed between scheduled releases when a critical bug or
security issue is found.

## Release Shepherd

Each release is owned by a **release shepherd** — one of the project
[maintainers](../README.md#maintainers). The shepherd is responsible for
driving the release end-to-end: creating the release checklist issue, running
the preparation workflow, coordinating review, and verifying the final release.

Shepherds rotate among maintainers across releases to share the load.

## Release Schedule

Releases follow a 4-week cadence.

| Version | Target Date | Release Shepherd                                             |
| ------- | ----------- | ------------------------------------------------------------ |
| v1.2.0  | 2026-10-12  | Yi Yang ([@y1yang0](https://github.com/y1yang0))             |
| v1.3.0  | 2026-11-09  | Azhar Momin ([@amazingakai](https://github.com/amazingakai)) |
| v1.4.0  | 2026-12-07  | Kemal Akkoyun ([@kakkoyun](https://github.com/kakkoyun))     |
| v1.5.0  | 2027-01-04  | **volunteer welcome**                                        |
| v1.6.0  | 2027-02-01  | **volunteer welcome**                                        |

If you are interested in volunteering to shepherd an upcoming release, please
create a pull request proposing yourself for that release.

## Before You Release

1. **Create a [Version Release](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/new?template=version_release.md) checklist issue**
   to track progress. Fill in the version number in the title.

2. **Ensure the target branch is green.** All required CI workflows must be
   passing on the commit you intend to release.

3. **Verify there are no pending breaking-change PRs** that should land before
   this release. Check for PRs labeled `breaking`.

4. **Confirm the version number.** This project follows
   [Semantic Versioning](https://semver.org/):
   - `PATCH` (`v1.x.Y+1`) — bug fixes, no API changes
   - `MINOR` (`v1.X+1.0`) — new features, backward-compatible
   - `MAJOR` (`vX+1.0.0`) — breaking changes

## Release Steps

### 1. Run the Prepare Release Workflow

1. Go to the [Actions tab](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/actions/workflows/release-prepare.yml).
2. Click **Run workflow**.
3. Enter the version (e.g. `v1.3.0`) and target branch (default: `main`).
   - If releasing `go.opentelemetry.io/otelc/pkg` alongside `otelc`, enable
     **Also release go.opentelemetry.io/otelc/pkg** (`release_pkg: true`) and
     specify **The pkg/ version to release** (`pkg_version`, e.g. `v0.1.0`).
     Releasing `pkg/` is optional because `pkg/hook` changes infrequently (see
     [Releasing the pkg Module](#releasing-the-pkg-module-goopentelemetryiootelcpkg)).
4. The workflow creates a temporary `release/<version>` branch, generates
   release notes into `.github/release-notes.md`, and opens a **draft PR**. The
   branch is used only for this release PR and can be deleted after the release
   workflow completes.

### 2. Review the Release PR

1. Open the draft PR created by the workflow.
2. Review the auto-generated release notes in `.github/release-notes.md`.
   - The notes contain a curated summary (`Breaking Changes`, `Added`, `Fixed`,
     `Changed`) above GitHub's complete `What's Changed` list.
   - Routine maintenance, test, and dependency PRs remain listed under
     `What's Changed`.
   - Edit the file directly in the PR to add context, reword entries, or
     highlight important changes.
3. When satisfied, mark the PR as **Ready for review** and request approvals.

### 3. Merge the Release PR

Merging the approved release PR automatically triggers the
[Release workflow](../.github/workflows/release.yml), which:

1. Tests the `pkg` module (`go test -C pkg ./...`) and creates/pushes the
   annotated git tag `pkg/<pkg_version>` if `pkg/` was included in the release PR
2. Cross-compiles `otelc` for all supported platforms (see
   [Cross-Compilation Targets](#cross-compilation-targets))
3. Generates SHA256 checksums for all binaries
4. Tags the merge commit with the version
5. Publishes the GitHub release with binaries, checksums, and the curated
   release notes. When `pkg/` is included, the release title combines both
   versions separated by `/` (e.g. `v1.3.0/v0.1.0`).

## Pre-Releases

To cut a pre-release (e.g. a release candidate for integration testing):

1. Run the Prepare Release workflow with a pre-release version, e.g.
   `v1.3.0-rc.1`.
2. Follow the same review and merge process.
3. The release will be marked as a **pre-release** on GitHub.

## Patch Releases (Hotfixes)

No permanent release or maintenance branches are kept. Critical bug fixes
should always land on `main` first. For patch releases between scheduled
releases:

1. If no unrelated changes have landed on `main` since the last release, simply
   run the Prepare Release workflow targeting `main` with the next patch
   version (e.g. `v1.2.1`).

2. If `main` has moved on with changes that should not be included, create a
   temporary hotfix branch `release/v<MAJOR>.<MINOR>.x` from the previous
   release tag and cherry-pick the fix commit from `main`:

   ```sh
   git fetch origin --tags
   git checkout -b release/v1.2.x v1.2.0
   git cherry-pick <fix-commit-from-main>
   git push origin release/v1.2.x
   ```

   Then run the Prepare Release workflow with `v1.2.1` targeting
   `release/v1.2.x`, review and merge the resulting release PR into
   `release/v1.2.x`, and delete the temporary branch once the release is
   published and verified.

## Releasing the pkg Module (`go.opentelemetry.io/otelc/pkg`)

The `go.opentelemetry.io/otelc/pkg` module contains `pkg/hook` and
`pkg/hook/hooktest`. Because `pkg/hook` has a strict additive compatibility
guarantee (see [`pkg/hook/README.md`](../pkg/hook/README.md)) and changes
infrequently, it is not tied to every `otelc` binary release. Releasing `pkg/`
is optional and requires explicitly providing a `pkg_version`.

There are two ways to release `pkg/`:

### 1. Alongside an `otelc` Binary Release

When running the [Prepare Release workflow](../.github/workflows/release-prepare.yml):

1. Set `release_pkg` to `true`.
2. Provide `pkg_version` (e.g. `v0.1.0`).
3. The workflow validates the version format, includes the `pkg/` release in the
   release PR title (e.g. `release: v1.3.0/v0.1.0`), and records the pkg version.
4. When the release PR is merged, the Release workflow runs `go test -C pkg ./...`,
   creates and pushes the annotated git tag `pkg/<pkg_version>`, and sets the GitHub
   release title to `<otelc_version>/<pkg_version>` (e.g. `v1.3.0/v0.1.0`).

### 2. Standalone Release (Manual Tagging)

Maintainers can also release `pkg/` independently at any time without cutting full
`otelc` platform binaries by testing and tagging the commit directly:

```sh
# 1. Run unit tests for pkg
go test -C pkg ./...

# 2. Create and push the annotated tag
git tag -a "pkg/v0.1.0" -m "Release pkg/v0.1.0"
git push origin "pkg/v0.1.0"

# 3. (Optional) Create a GitHub release for the tag
gh release create "pkg/v0.1.0" --title "pkg/v0.1.0" --generate-notes
```

This manual tagging approach can also be used if a `pkg/` release was omitted during
an earlier `otelc` binary release PR.

## Cross-Compilation Targets

`make build-all` compiles `otelc` for the following platforms:

| OS      | Architecture | Binary name                  |
|---------|--------------|------------------------------|
| Linux   | amd64        | `otelc-linux-amd64`          |
| Linux   | arm64        | `otelc-linux-arm64`          |
| macOS   | amd64        | `otelc-darwin-amd64`         |
| macOS   | arm64        | `otelc-darwin-arm64`         |
| Windows | amd64        | `otelc-windows-amd64.exe`    |

All binaries and a `checksums.txt` file are uploaded to the GitHub release as
downloadable assets.

## Post-Release

After the release is published:

1. **Verify the release artifacts.** Open the
   [Releases page](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/releases)
   and confirm:
   - All 5 platform binaries are listed as assets
   - The `checksums.txt` file is present
   - The release notes look correct

2. **Download and test a binary** for your platform:

   ```sh
   ./otelc version
   ```

   Confirm it prints the expected version, commit hash, and build date.

3. **Sync website documentation.** If the release includes user-facing changes
   (new features, changed behavior, new instrumentations), update the
   [compile-time instrumentation docs](https://opentelemetry.io/docs/zero-code/go/compile-time/)
   by opening a PR against the
   [opentelemetry.io repository](https://github.com/open-telemetry/opentelemetry.io/tree/main/content/en/docs/zero-code/go/compile-time).

4. **Announce the release** in the
   [#otel-go-compt-instr-sig](https://cloud-native.slack.com/archives/C088D8GSSSF)
   Slack channel with a link to the release notes.

5. **Close the release checklist issue.**

## Breaking Changes

Breaking changes are detected automatically. When a PR title uses the
conventional commit `!` modifier (e.g. `feat!: remove deprecated API` or
`fix(scope)!: change default behavior`), the `breaking` label is added
automatically by CI. This label controls categorization in the auto-generated
release notes.
