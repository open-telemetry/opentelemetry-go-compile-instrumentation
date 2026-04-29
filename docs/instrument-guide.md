# Adding a New Instrumentation Hook

This guide outlines the workflow for adding compile-time instrumentation for a third-party library.

The process consists of three main steps:

1. **Define Rules**: Create a YAML file to match the target package and function.
2. **Implement Hooks**: Write the `Before` and `After` hook functions in Go.
3. **Verify**: Add tests to ensure the instrumentation works as expected.

---

## 1. Define Rules

Rules are defined in YAML format and stored in `pkg/instrumentation/<library-name>/<library-name>.yaml`. This file tells `otelc` which functions to instrument.

Create a new file `pkg/instrumentation/<library-name>/<library-name>.yaml`. Below is an example configuration for instrumenting a function `NewServer`:

```yaml
inject_to_grpc_newserver:
  target: google.golang.org/grpc
  version: v1.63.0,v1.70.0
  where:
    func: NewServer
  do:
    - inject_hooks:
        before: BeforeNewServer
        after: AfterNewServer
        path: github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/grpc/server
```

- `target`: Import path of the package to instrument.
- `version`: Version range to match. The left bound is inclusive, the right bound is exclusive. If version is not specified, the rule is applicable to all versions.
- `where`: Non-package selectors. `func` names the function to hook.
- `do`: Ordered list of modifiers. `inject_hooks` declares this rule type and carries:
  - `before` / `after`: names of the hook functions.
  - `path`: import path where the hook functions are defined.

> [!NOTE]
> The 2-tier `where`/`do` schema and all other rule types are documented in [rules.md](rules.md). The schema invariants are recorded in [ADR-0003](adr/0003-structured-rule-schema.md).

## 2. Implement Hooks

Hook functions are standard Go functions. We place them in the package specified by the `path` field in the rule YAML.

### Hook Definition

The first parameter must always be `inst.HookContext`.

- **Before Hook**: Parameters match the target function's arguments.
- **After Hook**: Parameters match the target function's return values.

Target function:

```go
func NewServer(opts ...grpc.ServerOption) *grpc.Server
```

Hook implementation:

```go
package server

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"google.golang.org/grpc"
)

// BeforeNewServer matches the arguments of NewServer
func BeforeNewServer(ictx inst.HookContext, opts ...grpc.ServerOption) {
	// Logic to execute before the original function
}

// AfterNewServer matches the return value of NewServer
func AfterNewServer(ictx inst.HookContext, server *grpc.Server) {
	// Logic to execute after the original function
}
```

If we cannot import a specific type (e.g., it is unexported), we can use `interface{}` in the hook signature.

### Limitations

When implementing hooks, we must adhere to certain limitations:

1. **Restricted Imports**: If we are instrumenting a library (e.g., `github.com/foo/bar`), our hook code can only import from:
   - The Target Library (`github.com/foo/bar`)
   - OpenTelemetry packages
   - Standard Library packages

   Importing other third-party libraries is not allowed.

2. **Generic Functions**: If the target function is generic, we cannot use `HookContext` APIs to modify parameters or return values (e.g., `SetParam`, `SetReturnVal`).

### GLS Operation for OTel SDK Instrumentation

This section explains how goroutine-local storage (GLS) is used by the OTel SDK instrumentation.

#### Background

The OTel SDK normally propagates span context via `context.Context`. Some code paths still call APIs such as `trace.SpanFromContext(context.Background())`, where no span exists in the provided context.

To improve compatibility, this project stores the active span chain in goroutine-local storage and bridges selected OTel SDK APIs to that state during compile-time instrumentation.

#### High-Level Flow

The GLS flow is implemented through three parts:

1. Runtime GLS fields and helpers in the instrumented runtime package.
2. Injected OTel SDK trace helper file (`otel_trace_context.go`).
3. Hook rules that add/remove/read spans at key OTel SDK call sites.

At runtime:

- On span creation (`newRecordingSpan`, `newNonRecordingSpan`), the new span is added to GLS.
- On span end (`recordingSpan.End`, `nonRecordingSpan.End`), the span is removed from GLS.
- On `trace.SpanFromContext`, if the original return span is invalid, the hook tries GLS as a fallback.

#### Main Components

##### 1) Runtime GLS accessors

`pkg/instrumentation/runtime/runtime_gls.go` provides low-level accessors:

- `GetTraceContextFromGLS()`
- `SetTraceContextToGLS(interface{})`
- `GetBaggageContainerFromGLS()`
- `SetBaggageContainerToGLS(interface{})`

It also defines `OtelContextCloner` for goroutine propagation logic.

##### 2) Injected trace context holder

`pkg/instrumentation/otel/sdk/trace/otel_trace_context.go` defines an internal linked-list based trace context container in GLS:

- add span to current goroutine context
- delete span when ended
- fetch current span for fallback lookup

The max chain size is configurable:

- env var: `OTEL_GLS_MAX_SPANS`
- default: `1000`
- invalid or non-positive values are ignored (default remains in effect)

##### 3) Hook integration points

Configured in `pkg/instrumentation/otel/hook/hooks.yaml` and implemented in `pkg/instrumentation/otel/hook/`:

- `tracer_setup.go`: add span to GLS after span creation
- `span_setup.go`: remove span from GLS before span end
- `span_context.go`: fallback to GLS in `trace.SpanFromContext`

#### Why GLS is Needed

GLS fallback is useful for compatibility with existing code that:

- does not pass context through all call boundaries
- uses `context.Background()` at read points
- expects current span lookup to still work in instrumented binaries

This is especially helpful for auto-instrumentation scenarios where user code is unchanged.

#### Operational Notes

- GLS state is scoped to a goroutine. Correct context propagation across goroutines still depends on runtime propagation hooks.
- The fallback behavior only applies where configured by instrumentation rules.
- This mechanism is intended for compile-time instrumentation internals; it is not a public API contract.

## 3. Testing

### Unit Tests

We verify the instrumentation through unit and integration tests.

Create standard Go tests (`*_test.go`) alongside the hook functions to verify logic.

```bash
go test ./pkg/instrumentation/<library>/...
```

### Integration Tests

Integration tests run the instrumented code to ensure hooks are triggered correctly. These are located in `test/integration/`.

We should:

- Build the test app with the `otelc` tool and run the produced binary. The binary must live under `test/apps/<name>/...`
- Assert exported telemetry (traces/spans).
- Validate semantic conventions (required + recommended attributes) for the spans created by the instrumentation.

To run integration tests:

```bash
make test-integration
```

## 4. Verify

Check that your instrumentation package have following elements:

- A rule YAML `pkg/instrumentation/<library-name>/<library-name>.yaml` with a correct `target` and version range.
- Hook implementation under `pkg/instrumentation/<library>/...`
- Unit tests alongside the hooks for logic-level behavior.
- Integration tests in `test/integration/` that execute an instrumented binary and validate spans/attributes.
