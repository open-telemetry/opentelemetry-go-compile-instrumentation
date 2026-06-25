## Description

This PR establishes unit-test coverage >=70% for both `tool` and `pkg` module trees, and enables Codecov status checks to actively enforce this gate (failing on regressions).

Specifically, this PR introduces:
- `TestMerge` and `TestMergeTraceIgnored` unit tests to `tool/internal/profile/profile_test.go` to test the `Merge` function. This increases `tool/internal/profile` coverage from **48%** to **71%**.
- `TestShutdown` and `TestSetupOpenTelemetry` unit tests to `pkg/runtime/otel_setup_test.go` to cover `Initialize`, `Shutdown`, and `setupOpenTelemetry` functions. This increases `pkg/runtime` coverage from **60.3%** to **76%**.
- Disabling the `informational` flag in `codecov.yml` for both `tool` and `pkg` flags to make the 70% target an enforcing status check.

## Motivation

This resolves the outstanding task in tracking issue #569 to establish and enforce unit test coverage gates. Both packages now consistently meet the target threshold, enabling us to turn on gate enforcement in CI.

Fixes #569

---

## Checklist

- [x] PR title follows [conventional commits](https://www.conventionalcommits.org/) format
- [x] Code formatted: `make format`
- [x] Linters pass: `make lint`
- [x] Tests pass: `make test`
- [x] Tests added for new functionality
- [x] Tests follow [testing guidelines](docs/testing.md)
- [x] Documentation updated (if applicable)
