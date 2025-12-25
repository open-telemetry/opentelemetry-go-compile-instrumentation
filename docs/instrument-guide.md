# Adding a New Instrumentation Hook

This guide outlines the workflow for adding compile-time instrumentation for a third-party library.

The process consists of three main steps:

1. **Define Rules**: Create a YAML file to match the target package and function.
2. **Implement Hooks**: Write the `Before` and `After` hook functions in Go.
3. **Verify**: Add tests to ensure the instrumentation works as expected.

---

## 1. Define Rules

Rules are defined in YAML format and stored in `tool/data/`. These files tell the `otel` which functions to instrument.

Create a new file `tool/data/<library-name>.yaml`. Below is an example configuration for instrumenting a function `NewServer`:

```yaml
inject_to_grpc_newserver:
  target: google.golang.org/grpc
  version: v1.63.0,v1.70.0
  func: NewServer
  before: BeforeNewServer
  after: AfterNewServer
  path: github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/grpc/server
```

* `target`: Import path of the package to instrument.
* `version`: Version range to match. The left bound is inclusive, the right bound is exclusive. If version is not specified, the rule is applicable to all versions.
* `func`: Name of the function to hook.
* `before` / `after`: Names of the hook functions.
* `path`: Import path where the hook functions are defined.

> [!NOTE]
> In addition to function rules, there are other types of rules available. For detailed information on these, refer to [rules.md](rules.md).

## 2. Implement Hooks

Hook functions are standard Go functions. We place them in the package specified by the `path` field in the rule YAML.

### Hook Definition

The first parameter must always be `inst.HookContext`.

* **Before Hook**: Parameters match the target function's arguments.
* **After Hook**: Parameters match the target function's return values.

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
    * The Target Library (`github.com/foo/bar`)
    * OpenTelemetry packages
    * Standard Library packages

    Importing other third-party libraries is not allowed.

2. **Generic Functions**: If the target function is generic, we cannot use `HookContext` APIs to modify parameters or return values (e.g., `SetParam`, `SetReturnVal`).

## 3. Verify

We verify the instrumentation through unit and integration tests.

### Unit Tests

Create standard Go tests (`*_test.go`) alongside the hook functions to verify logic.

```bash
go test ./pkg/instrumentation/<library>/...
```

### Integration Tests

Integration tests run the instrumented code to ensure hooks are triggered correctly. These are located in `test/integration/`.

To run integration tests:

```bash
make test-integration
```
