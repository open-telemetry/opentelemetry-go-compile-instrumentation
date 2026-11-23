# Testing

This document describes the testing strategy for the project, the different test categories, and when to use each.

Tests are organized into two primary categories in this repository:

| Category | Location | Build Tag | Scope |
| :------- | :------- | :-------- | :---- |
| Unit | `tool/**/*_test.go`, `pkg/**/*_test.go` | none | Single function or component in isolation. |
| Integration | `test/integration/` | `integration` | Compiler engine, build cache, flag forwarding, and hook execution tests. |

> [!NOTE]
> End-to-end (E2E) application tests, LatestLib (`latestlibbuild`, `latestlibrun`), and VersionMatrix test suites for library instrumentation packages are maintained in [`open-telemetry/opentelemetry-go-compile-contrib`](https://github.com/open-telemetry/opentelemetry-go-compile-contrib).

## Unit Tests

> [!IMPORTANT]
> **When to write a unit test.** Any change to a single function, hook contract, or internal component. If the behavior can be validated without building an instrumented binary, it belongs here.

Unit tests live next to the source code they exercise and require no build tags.

There are two main areas:

- **Tool tests** (`tool/`). Cover the compile-time instrumentation pipeline: AST rewriting, import resolution, trampoline generation, package loading, setup logic, and manifest discovery. Golden-file tests in `tool/internal/instrument/` snapshot expected output and can be updated with `make test-unit/update-golden`.
- **Package tests** (`pkg/`). Cover the public hook interface contract (`pkg/hook.HookContext`) and runtime helper types.

### Golden-test helper packages

A golden testcase directory under `tool/internal/instrument/testdata/golden/<name>/` may contain a `helpers/` subdirectory with one or more Go packages. The test harness automatically discovers each subdirectory, compiles it into a `.a` archive, and registers it in the `importcfg` so the instrumented source can import it at compile time.

Use this convention when a testcase exercises call rules that reference wrapper functions from an external (non-stdlib) package via the `imports:` field. To add a new helper:

1. Create `helpers/<pkgname>/<pkgname>.go` with the wrapper code (package name must match the directory name).
2. Reference the full import path in `rules.yml` under `imports:`, the path is `<root-module>/tool/internal/instrument/testdata/golden/<testname>/helpers/<pkgname>`.
3. Create a placeholder for the golden file and run `make test-unit/update-golden` to regenerate the `.golden` snapshot.

### Fuzz targets

Go fuzz targets (`func FuzzXxx(f *testing.F)`) live next to the code they exercise and count as unit tests. They suit components that parse untrusted or free-form input, where the surface is too large to enumerate by hand — compiler directives, flags, and configuration formats.

Prefer a fuzz target when there is an invariant worth stating mechanically rather than a specific input worth asserting on. Two kinds are useful here:

- **It never panics.** The instrumentation tool parses code during compilation, so a panic would break the user's build rather than degrading gracefully.
- **Values survive the parse/serialize cycle.** Round-trip transformations should produce predictable structures.

A fuzz target's seed corpus runs as an ordinary table test during `make test-unit`, so seeds guard their cases without anyone passing `-fuzz`. Add a seed for every bug a fuzzer finds. Run a target as a real fuzzer locally with:

```bash
go test ./tool/internal/ast/ -run=Fuzz -fuzz=FuzzScanArgs -fuzztime=60s
```

Keep the invariant honest: assert it only where it genuinely holds. Malformed input may legitimately produce a result the next component cannot use, as long as that component degrades gracefully.

## Integration Tests

> [!IMPORTANT]
> **When to write an integration test.** Any change to the tool's code injection, build-step wrapping, compiler/linker flag handling, caching mechanisms, or `HookContext` API. Integration tests build complete binaries with `otelc go build` and assert on binary execution, build cache reuse, or exported spans.

Integration tests under `test/integration/` exercise the full compilation and execution flow without external network dependencies:

- **Hook execution**: `basic_test.go` verifies basic span creation and context propagation with `pkg/hook`.
- **Compiler & Linker flags**: `toolexec_goflags_test.go`, `gcflags_build_test.go`.
- **Build caching & Workspaces**: `work_dir_test.go`, `session_resolution_test.go`, `warm_cache_added_import_test.go`.
- **Packaging & Modules**: `vendor_test.go`, `pin_test.go`, `pgo_build_test.go`, `nested_added_import_test.go`.
- **Rule loading & Composition**: `instrumentation_selection_test.go`.

Test applications are built on demand by the tests that use them. Each top-level test builds its required application once (via `otelc go build`) and reuses the resulting binary across its subtests when applicable.

CI shards the integration suite by top-level test-name partitions through `INTEGRATION_TEST_RUN`; `SHARD_TOTAL` controls the shard count, and new tests are assigned automatically without per-test workflow changes.

When `OTELC_TEST_GOCACHE` is set, standard `test/apps` builds use a per-app
Go build cache at `${OTELC_TEST_GOCACHE}/${app}` and drop `-a` so warm builds can
reuse cached compile outputs. Custom app directories do not use this cache and
keep caller-supplied build arguments unchanged.

## Test Applications

Minimal applications in `test/apps/` (`customrules`, `gcflags`, `pgo`, `prepared`, `vendored`, `workdir`, `injecteddeps`, `nestedimports`) serve as standalone targets for `otelc go build` scenarios.

Shared helpers in `test/testutil/` provide the in-memory OTLP collector, build/run wrappers, and test assertion helpers used by integration tests.

## Running Tests

> [!NOTE]
> Integration tests require `make build` (and `make build-demo` for demos) before running.

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

# Coverage
make test-unit/coverage
make test-integration/coverage
```

## Coverage

> [!NOTE]
> The project enforces a **≥68% unit-test coverage** floor for the `tool/` module tree and **≥70%** for the `pkg/` module tree.
> Codecov checks each tree against its target and **blocks the PR** when coverage drops below it.

Run the same gate locally with:

```sh
make check-coverage
```

Set `TOOL_COVERAGE_THRESHOLD` or `PKG_COVERAGE_THRESHOLD` if you need to validate a different floor while experimenting locally.

### Coverage target rationale

The coverage floors were set to reflect the current unit-test reachability of each module tree.
The `tool/` flag targets 68% because the remaining build-orchestration paths are exercised through
integration coverage, while `pkg/` remains at 70%. Coverage is tracked **per module tree** so that
one area cannot mask regression in the other.

### CI behaviour

The `test-unit-coverage` job in `.github/workflows/test-unit.yaml`:

1. Runs `make test-unit/coverage` to generate `coverage-tool.txt` and `coverage-pkg.txt`.
2. Uploads both files to Codecov for historical tracking (flags: `tool`, `pkg`).

Codecov evaluates each flag against the target defined in `.github/codecov.yml` and posts the result as an
**enforcing** status check (`informational: false`): a coverage shortfall below the target fails
the check and blocks the PR.

All Go test commands use `-shuffle=on` and `-count=1` to avoid ordering issues and Go test result caching. Integration tests may still reuse the separate per-app Go build cache described above.

CI runs each category in a separate workflow across Linux (amd64/arm64), macOS (arm64), and Windows (amd64). See `.github/workflows/test-*.yaml` for details.

## Writing New Tests

1. **Pick the right category.** Unit tests for single packages/functions in `tool/` or `pkg/hook`; integration tests under `test/integration/` for full `otelc` build behaviors.
2. **Follow existing patterns.** Table-driven tests for unit tests, self-contained fixtures with `copyTestApp` for integration tests.
3. **Add a test app if needed.** If existing fixtures under `test/apps/` don't cover your scenario, add a minimal standalone module there.
