# Testing

This document describes the testing strategy for the project, the different test categories, and when to use each.

Tests are organized in three categories, each with a distinct purpose and scope.

| Category | Location | Build Tag | Scope |
| :------- | :------- | :-------- | :---- |
| Unit | `tool/**/*_test.go`, `pkg/**/*_test.go` | none | Single function or component in isolation. |
| Integration | `test/integration/` | `integration` | Instrumented binary against a local or in-process dependency. |
| E2E | `test/e2e/` | `e2e` | Full client-server flow validating context propagation across services. |

## Unit Tests

Unit tests live next to the source code they exercise and require no build tags.

There are two main areas:

- **Tool tests** (`tool/`). Cover the compile-time instrumentation pipeline: AST rewriting, import resolution, trampoline generation, package loading, and setup logic. Golden-file tests in `tool/internal/instrument/` snapshot expected output and can be updated with `make test-unit/update-golden`.
- **Package tests** (`pkg/`). Cover the runtime instrumentation hooks and semantic convention helpers. Each hook package has tests that verify span creation, context propagation, error recording, and the enable/disable mechanism via `OTEL_GO_ENABLED_INSTRUMENTATIONS` / `OTEL_GO_DISABLED_INSTRUMENTATIONS`.

**When to write a unit test.** Any change to a single function, hook, or internal component. If the behavior can be validated without building an instrumented binary, it belongs here.

## Integration Tests

Integration tests build real binaries with the `otel` tool and run them against local or in-process dependencies (e.g. `httptest.Server`, in-process gRPC server, miniredis, testdb driver).

Each test follows the same pattern:

1. Build the test application with compile-time instrumentation.
2. Start an in-memory OTLP collector.
3. Run the instrumented binary against a local dependency.
4. Assert on the exported spans and their semantic conventions.

**When to write an integration test.**

- **Tool hook changes.** Any change to the tool's code injection or the `HookContext` interface must be covered by `basic_test.go`. It exercises `pkg/instrumentation/basic/` and validates the foundational hook machinery that all other instrumentations rely on.
- **Instrumentation package changes.** Every package in `pkg/instrumentation/` must have a corresponding integration test. If you add or modify a hook, there should be an integration test that builds an instrumented binary and asserts on the exported spans for that component.

## E2E Tests

E2E tests verify that context propagation works across service boundaries. Both client and server are instrumented binaries communicating over the network, and traces must contain spans from both sides sharing the same trace ID.

Current coverage:

| Test | What it validates |
| :--- | :---------------- |
| `http_test.go` | HTTP client and server spans in a single trace (1 trace, 2 spans). |
| `grpc_test.go` | gRPC unary and streaming calls (2 traces, 2 spans each). |

**When to write an E2E test.** When you need to validate that traces propagate correctly between two instrumented services over a real network boundary.

## Test Applications

Minimal applications in `test/apps/` serve as instrumentation targets. Each is a standalone Go module that the test infrastructure builds with `otel go build`.

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

# Coverage
make test-unit/coverage
make test-integration/coverage
make test-e2e/coverage
```

All test commands use `-shuffle=on` and `-count=1` to avoid ordering issues and caching.

CI runs each category in a separate workflow across Linux (amd64/arm64), macOS (arm64), and Windows (amd64). See `.github/workflows/test-*.yaml` for details.

## Writing New Tests

1. **Pick the right category.** Use the decision tree below.
2. **Follow existing patterns.** Table-driven tests for units, `TestFixture` for integration/E2E.
3. **Use semantic convention helpers.** `testutil.RequireHTTPClientSemconv`, `RequireGRPCServerSemconv`, etc.
4. **Add a test app if needed.** If the existing apps in `test/apps/` don't cover your case, add a new minimal module there.
