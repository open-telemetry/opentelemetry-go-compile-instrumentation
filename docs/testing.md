# Testing

This document describes the testing strategy for the project, the different test categories, and when to use each.

Tests are organized in five categories, each with a distinct purpose and scope.

| Category | Location | Build Tag | Scope |
| :------- | :------- | :-------- | :---- |
| Unit | `tool/**/*_test.go`, `pkg/**/*_test.go` | none | Single function or component in isolation. |
| Integration | `test/integration/` | `integration` | Instrumented binary against a local or in-process dependency. |
| E2E | `test/e2e/` | `e2e` | Multiple processes (e.g. client + server). |
| LatestLibBuild | `test/latestlibbuild/` | `latestlibbuild` | Compile instrumented app against `@latest` of each instrumented library. |
| LatestLibRun | `test/latestlibrun/` | `latestlibrun` | Run integration suite after bumping each instrumented library to `@latest`. |

## Unit Tests

> [!IMPORTANT]
> **When to write a unit test.** Any change to a single function, hook, or internal component. If the behavior can be validated without building an instrumented binary, it belongs here.

Unit tests live next to the source code they exercise and require no build tags.

There are two main areas:

- **Tool tests** (`tool/`). Cover the compile-time instrumentation pipeline: AST rewriting, import resolution, trampoline generation, package loading, and setup logic. Golden-file tests in `tool/internal/instrument/` snapshot expected output and can be updated with `make test-unit/update-golden`.
- **Package tests** (`pkg/`). Cover the runtime instrumentation hooks and semantic convention helpers. Each hook package has tests that verify span creation, context propagation, error recording, and the enable/disable mechanism via `OTEL_GO_ENABLED_INSTRUMENTATIONS` / `OTEL_GO_DISABLED_INSTRUMENTATIONS`.

### Golden-test helper packages

A golden testcase directory under `tool/internal/instrument/testdata/golden/<name>/` may contain a `helpers/` subdirectory with one or more Go packages. The test harness automatically discovers each subdirectory, compiles it into a `.a` archive, and registers it in the `importcfg` so the instrumented source can import it at compile time.

Use this convention when a testcase exercises call rules that reference wrapper functions from an external (non-stdlib) package via the `imports:` field. To add a new helper:

1. Create `helpers/<pkgname>/<pkgname>.go` with the wrapper code (package name must match the directory name).
2. Reference the full import path in `rules.yml` under `imports:`, the path is `<root-module>/tool/internal/instrument/testdata/golden/<testname>/helpers/<pkgname>`.
3. Create a placeholder for the golden file and run `make test-unit/update-golden` to regenerate the `.golden` snapshot.

## Integration Tests

> [!IMPORTANT]
> **When to write an integration test.**
>
> - **Tool hook changes.** Any change to the tool's code injection or the `HookContext` interface must be covered by `basic_test.go`. It exercises `demo/app/basic/instrumentation/` and validates the foundational hook machinery that all other instrumentations rely on.
> - **Instrumentation package changes.** Every package in `instrumentation/` must have a corresponding integration test. If you add or modify a hook, there should be an integration test that builds an instrumented binary and asserts on the exported spans for that component.

Integration tests build real binaries with the `otelc` tool and run them against **in-process** dependencies (e.g. `httptest.Server`, in-process gRPC server, miniredis, testdb driver).

Test applications are built on demand by the tests that use them. Each top-level test builds its required application once (via `otelc go build`) and reuses the resulting binary across its subtests when applicable.

Each test follows the same pattern:

1. Start an in-memory OTLP collector.
2. Run the instrumented binary against a local dependency.
3. Assert on the exported spans and their semantic conventions.

## E2E Tests

> [!IMPORTANT]
> **When to write an E2E test.** When the scenario involves multiple instrumented processes or services. Typical cases include context propagation across services, multi-service interactions or complex scenarios.

E2E tests spin up multiple processes (e.g. an instrumented client and an instrumented server) and verify they produce a coherent trace with spans from every participant sharing the same trace ID.

## Test Applications

Minimal applications in `test/apps/` serve as instrumentation targets. Each is a standalone Go module that the test infrastructure builds with `otelc go build`.

Shared helpers in `test/testutil/` provide the OTLP collector, build/run wrappers, readiness probes, and semantic convention assertion functions used by both integration and E2E tests.

## Running Tests

> [!NOTE]
> Integration and e2e tests require `make build` (and `make build-demo` for gRPC/HTTP demos) before running.

```bash
# All tests
make test

# Unit tests
make test-unit              # all unit tests
make test-unit/tool         # tool only
make test-unit/pkg          # pkg only
make test-unit/update-golden # update golden files

# Integration tests (requires: make build)
make test-integration

# E2E tests (requires: make build build-demo)
make test-e2e

# LatestLibBuild tests (requires: make build; mutates test/apps/*/go.mod — run git restore test/apps afterwards)
make test-latestlibbuild

# LatestLibRun tests (requires: make build; mutates test/apps/*/go.mod — run git restore test/apps afterwards)
make test-latestlibrun

# Coverage
make test-unit/coverage
make test-integration/coverage
make test-e2e/coverage
```

## Coverage

> [!NOTE]
> The project targets a **≥70% unit-test coverage** goal for both the `tool/` and `pkg/` module trees.
> This is an **aspirational target** currently being worked toward. The CI step reports coverage
> for both trees but does **not yet block merges** when coverage is below the threshold.
> Once the target is consistently met, the gate will be promoted to enforcement.

### Coverage target rationale

The 70% floor is the minimum bar agreed in [issue #569](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/569)
(tracked under the release 1.0.0 roadmap). Coverage is tracked **per module tree** — `tool/` and
`pkg/` are checked independently so that one area cannot mask regression in the other.

### CI behavior

The `test-unit-coverage` job in `.github/workflows/test-unit.yaml`:

1. Runs `make test-unit/coverage` to generate `coverage-tool.txt` and `coverage-pkg.txt`.
2. Uploads both files to Codecov for historical tracking (flags: `tool`, `pkg`).

Codecov evaluates each flag against the 70% target defined in `codecov.yml` and posts the result
as an **informational** status check — a coverage shortfall is visible in the PR but does **not**
block merges. Flip `informational: false` in `codecov.yml` once the target is consistently met.

All test commands use `-shuffle=on` and `-count=1` to avoid ordering issues and caching.

CI runs each category in a separate workflow across Linux (amd64/arm64), macOS (arm64), and Windows (amd64). See `.github/workflows/test-*.yaml` for details.

## LatestLibBuild Tests

> [!IMPORTANT]
 > **What this test does.** For each app under `test/apps/`, the test discovers the app's direct dependencies whose module paths match an instrumentation rule `target:`, bumps those dependencies to `@latest`, and verifies that `otelc go build` still succeeds. It is a **compile-only** check — no binary is executed and no spans are asserted. It runs on every pull request and blocks merge if it fails.

| Category | Location | Build Tag | Scope |
| :------- | :------- | :-------- | :---- |
| LatestLibBuild | `test/latestlibbuild/` | `latestlibbuild` | Compile instrumented app against `@latest` of each library. |

### What a failure means

A failure means that the latest release of an upstream library introduced a **compile-time API break** that is incompatible with the current instrumentation hook. The remediation is:

1. Cap the existing rule's version range in the relevant `instrumentation/.../*.yaml` file (e.g. change the version field from `v1.2.3` to `v1.2.3,v4.5.6`).
2. Open a new rule entry covering `v4.5.6,` and implement the updated hook.

## LatestLibRun Tests

> [!IMPORTANT]
> **What this test does.** For each app under `test/apps/`, the test bumps the app's instrumented direct dependencies to `@latest` (same mutation as LatestLibBuild), then runs the full integration suite against those bumped modules. It is a **compile + run + span-assertion** check. It runs on a weekly schedule and on demand via `workflow_dispatch`. When it fails on `main`, the CI workflow automatically opens or updates a GitHub issue labelled `latestlibrun-failure`.

| Category | Location | Build Tag | Scope |
| :------- | :------- | :-------- | :---- |
| LatestLibRun | `test/latestlibrun/` | `latestlibrun` | Run integration suite after bumping each library to `@latest`. |

### What a failure means

A failure means that the latest release of an upstream library introduced a **runtime behavior change**, for example a removed function, a changed signature, or a dropped instrumentation hook point that breaks existing span assertions. The remediation is:

1. Cap the existing rule's version range in the relevant `instrumentation/.../*.yaml` file.
2. Open a new rule entry covering the new version range and update the hook implementation.
3. Close the auto-opened `latestlibrun-failure` issue once the fix lands on `main`.

## Writing New Tests

1. **Pick the right category.** Use the decision tree below.
2. **Follow existing patterns.** Table-driven tests for units, `TestFixture` for integration/E2E.
3. **Use semantic convention helpers.** `testutil.RequireHTTPClientSemconv`, `RequireGRPCServerSemconv`, etc.
4. **Add a test app if needed.** If the existing apps in `test/apps/` don't cover your case, add a new minimal module there.
