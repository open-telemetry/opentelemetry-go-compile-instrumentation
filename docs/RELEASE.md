# Release Process

This document describes how to cut a new release of
`opentelemetry-go-compile-instrumentation`.

## Release Cadence

Releases are cut approximately every **6 weeks**, following the broader
OpenTelemetry release cadence. Patch releases (hotfixes) are cut as needed
between scheduled releases when a critical bug or security issue is found.

## Release Shepherd

Each release is owned by a **release shepherd** — one of the project
[maintainers](../README.md#maintainers). The shepherd is responsible for
driving the release end-to-end: creating the release checklist issue, running
the preparation workflow, coordinating review, and verifying the final release.

Shepherds rotate among maintainers across releases to share the load.

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

1. Tags the merge commit with the version
2. Cross-compiles `otelc` for all supported platforms (see
   [Cross-Compilation Targets](#cross-compilation-targets))
3. Generates SHA256 checksums for all binaries
4. Publishes the GitHub release with binaries, checksums, and the curated
   release notes

## Pre-Releases

To cut a pre-release (e.g. a release candidate for integration testing):

1. Run the Prepare Release workflow with a pre-release version, e.g.
   `v1.3.0-rc.1`.
2. Follow the same review and merge process.
3. The release will be marked as a **pre-release** on GitHub.

## Patch Releases (Hotfixes)

No permanent release or maintenance branches are kept. For critical bug fixes
between scheduled releases:

1. If the fix is on `main` and no unrelated changes have landed since the last
   release, simply run the Prepare Release workflow targeting `main` with the
   next patch version (e.g. `v1.2.1`).

2. If `main` has moved on with changes that should not be included, create a
   temporary hotfix branch from the previous release tag and cherry-pick the
   fix onto it. For example:

   ```sh
   git fetch origin --tags
   git checkout -b release/v1.2.x v1.2.0
   git cherry-pick <fix-commit-sha>
   git push origin release/v1.2.x
   ```

   Then run the Prepare Release workflow with `v1.2.1` targeting
   `release/v1.2.x`. The branch exists only to prepare and publish this hotfix;
   it is not a persistent maintenance branch. Delete it after the hotfix release
   is published and verified.

3. After the patch release, open a PR to merge the fix back to `main` to avoid
   regression.

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
