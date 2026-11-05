# Hook Package (`go.opentelemetry.io/otelc/pkg/hook`)

`pkg/hook` defines the `HookContext` interface and supporting primitives used by compile-time instrumentation packages.

## Architecture Context

Instrumentation packages in `otelc` intercept function calls before and after execution. When a hook function runs, it receives an instance of `hook.HookContext`, which exposes methods to inspect parameters, modify return values, share data between pre- and post-hooks, or cancel/skip the intercepted call.

The lifecycle and distribution of this interface follow a unique dual-representation architecture:

1. **Source Interface:** The interface is authored in `pkg/hook/context.go`.
2. **Template Mirror:** `pkg/hook/context.go` is mirrored directly to `tool/internal/instrument/api.tmpl`. CI enforces byte-for-byte synchronization between these files via `make check-api-sync`.
3. **Concrete Implementation:** During compilation, `otelc` injects a concrete implementation struct, `HookContextImpl`, generated from `tool/internal/instrument/impl.tmpl` into the target package.

Because Go interfaces are satisfied **structurally** (compile-time interface satisfaction without explicit declarations), any concrete `HookContextImpl` emitted by `otelc` must implement all methods of the `hook.HookContext` interface expected by the compiled instrumentation package.

## API Stability and Compatibility Policy

Because `pkg/hook` can be imported by third-party and contrib instrumentation packages that are versioned and distributed independently from `otelc` itself, maintaining strict backward and forward compatibility is critical.

### Additive-Only Evolution in v1

Within major version `v1`, changes to `HookContext` must be **strictly additive**:

- New methods may be appended to the `HookContext` interface in `pkg/hook/context.go`.
- Whenever a method is added, it must be mirrored to `tool/internal/instrument/api.tmpl` and a corresponding implementation must be added to `HookContextImpl` in `tool/internal/instrument/impl.tmpl`.
- **Backward Compatibility:** Older instrumentations compiled against an earlier release of `pkg/hook` continue to function without recompilation. A Go interface requires only that the concrete struct implement the methods in that interface definition; adding new methods to `HookContextImpl` does not prevent it from satisfying an older, smaller `HookContext` interface.
- **Minimum Compiler Version:** When an instrumentation package adopts a newly added method, its author must specify the corresponding minimum `version` constraint in `otelc.yaml`. This ensures older `otelc` tool versions (which emit a `HookContextImpl` lacking the new method) will gracefully decline to instrument rather than failing at compile time.

### No Hard Removals

- **Never delete or mutate existing method signatures** in `v1`.
- Deleting an existing method or altering its parameter/return types would break structural interface satisfaction for already-published instrumentation packages compiled against earlier versions of `pkg/hook`, resulting in build failures.

### Soft Deprecation (Zero Runtime Overhead)

If a method in `HookContext` becomes obsolete or is superseded by a newer API:

1. **Deprecation Notice:** Mark the method with a standard Go deprecation comment (`// Deprecated: ...`) in `pkg/hook/context.go` (and sync it to `api.tmpl`).
2. **Stub Implementation:** Retain the method on `HookContextImpl` in `tool/internal/instrument/impl.tmpl` as a no-op or returning zero values.

**Zero Performance Impact:**
Soft deprecation in this architecture carries zero runtime cost:
- **0 struct fields:** Retaining a stub method requires no additional state or fields on `HookContextImpl`.
- **0 heap allocations:** No memory is allocated by unused stub methods.
- **0 CPU overhead:** An uncalled stub method is never executed, adding zero instruction overhead to hot execution paths.

### Major Breaking Changes (v2)

If a breaking change ever becomes unavoidable:

- It must be published under a new major module or package path (for example, `go.opentelemetry.io/otelc/pkg/v2/hook` or `pkg/hook/v2`).
- To avoid breaking the instrumentation ecosystem, `otelc` must support backward compatibility by providing dual code generation capable of emitting both v1 and v2 `HookContextImpl` implementations depending on the interface version imported by each target instrumentation.
