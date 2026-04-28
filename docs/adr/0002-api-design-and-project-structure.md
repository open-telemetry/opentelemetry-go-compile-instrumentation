# 2. API Design and Project Structure

Date: 2026-03-19

## Status

Accepted

## Context

Compile-time instrumentation requires a clear contract between the tool that rewrites code and the hook functions that are injected. Without a stable API design, adding new instrumentations becomes difficult to reason about and the generated code becomes fragile.

The project also needs a project layout that separates the compile-time tool from the runtime instrumentation packages, since they have fundamentally different lifecycles and dependency graphs.

## Decision

The full design is documented in [`docs/api-design-and-project-structure.md`](../api-design-and-project-structure.md). The key decisions captured here are:

**Hook model**: Instrumentation is expressed as pairs of plain Go functions — `Before*` and `After*` hooks — injected around target function calls via AST rewriting. This is simpler than an instrumenter hierarchy and avoids heavy abstractions.

**HookContext interface**: A single interface (`inst.HookContext`) is injected as the first parameter of every hook function. It provides `GetParam`/`SetParam` for reading and modifying target function arguments, and `GetKeyData`/`SetKeyData` for passing state between `Before` and `After` hooks.

**Semantic conventions as pure functions**: Attribute extraction helpers live in `semconv` sub-packages and are pure functions. They have no side effects and no dependency on the hook lifecycle.

**Two-phase build**:
1. *Setup* (`tool/internal/setup`): Discovers dependencies, matches hook rules, generates `otel_import.go`, and runs `go mod tidy`.
2. *Instrument* (`tool/internal/instrument`): Intercepts compilation via `-toolexec`, rewrites source files with AST manipulation using `github.com/dave/dst`, and compiles the modified files.

**Project layout**: `pkg/` contains all runtime packages (public API, instrumentation implementations). `tool/` contains the compile-time tool. They are separate Go modules so that instrumented applications do not transitively depend on tool internals.

## Consequences

- Adding a new library instrumentation is straightforward: implement `Before`/`After` functions, create a `semconv` package, and add a YAML rule file.
- The `HookContext` interface is the stability boundary. Changes to it require regenerating all hook call sites.
- Using `github.com/dave/dst` instead of `go/ast` preserves comments and formatting but introduces a dependency on a third-party AST library.
- The two-phase approach means instrumentation is baked into the binary at compile time — zero runtime overhead — at the cost of a modified build invocation (`otelc build` instead of `go build`).
- Separate `pkg/` and `tool/` modules prevent accidental tight coupling between runtime and compile-time code.
