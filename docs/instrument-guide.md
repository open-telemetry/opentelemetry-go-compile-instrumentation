# Adding a New Instrumentation Hook

This guide covers the normal workflow for adding compile-time instrumentation for a package:

1. define the rule YAML
2. implement the hook functions
3. add tests and run verification

## 1. Define the Rule

Rules live alongside the instrumentation package under `pkg/instrumentation/...`.

Example:

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

Field meanings:

- `target`: import path of the package to instrument
- `version`: optional version range; lower bound inclusive, upper bound exclusive
- `where.func`: function to hook
- `do`: ordered list of modifier entries
- `inject_hooks.before` / `inject_hooks.after`: hook names
- `inject_hooks.path`: package that contains the hook code

Notes:

- `target` and `version` stay top-level.
- Non-package selectors go under `where`.
- `do` is always a list, even when there is only one modifier.

See [rules.md](rules.md) for the full rule surface, including `where.file`, qualifier composition, and non-hook modifiers.

## 2. Implement the Hooks

Hook functions are ordinary Go functions placed in the package named by `path`.

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

func BeforeNewServer(ictx inst.HookContext, opts ...grpc.ServerOption) {
	// Runs before NewServer.
}

func AfterNewServer(ictx inst.HookContext, server *grpc.Server) {
	// Runs before NewServer returns.
}
```

Hook signature rules:

- the first parameter must be `inst.HookContext`
- `before` parameters must match the target function arguments
- `after` parameters must match the target function return values
- if a target type cannot be imported, `interface{}` is acceptable

## 3. Observe Hook Constraints

Important limitations:

1. Hook code for an instrumented library may only import:
   - the target library
   - OpenTelemetry packages
   - Go standard library packages
2. For generic target functions, `HookContext` mutation helpers such as `SetParam` and `SetReturnVal` are not supported.

## 4. Add Tests

Unit tests live next to the hook implementation:

```bash
go test ./pkg/instrumentation/<library>/...
```

Integration tests live under `test/integration/`. They should build and execute an instrumented binary, then assert on the emitted telemetry.

To run integration coverage:

```bash
make test-integration
```

## 5. Verify

Before sending the change out, confirm that you have:

- a rule YAML under `pkg/instrumentation/<library>/...` with the correct `target` and optional `version`
- hook implementation under `pkg/instrumentation/<library>/...`
- unit tests for hook behavior
- integration coverage when the change affects end-to-end telemetry behavior

Repository policy expects full verification with:

```bash
make all
```
