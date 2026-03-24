# 3. Join Point Filter Interface

Date: 2026-03-19

## Status

Accepted

## Context

The tool supports flat, single-predicate rules: one `func`, `struct`, `call`, `raw`, or
`file` field per rule. There is no way to compose multiple conditions — for example,
"instrument functions annotated with `//otelc:span`" — or to filter on package-level
properties such as import path patterns.

Orchestrion (DataDog's equivalent tool) uses an Aspect-Oriented Programming model with a
three-phase `Point` interface: `PackageMayMatch`, `FileMayMatch`, and `Matches`. The first
two phases allow cheap early-exit before source files are parsed.

## Decision

Introduce an optional `where` clause to rules. The clause holds a `FilterDef` (YAML) that
is compiled into a `Filter` interface once per rule before source-file iteration in
`preciseMatching`, and evaluated per source file.

```go
type Filter interface {
    Match(ctx *MatchContext) bool
}
```

`MatchContext` carries import path, source file path, and the parsed AST. Filters are built
once per rule and evaluated once per source file — not once per invocation. The `where`
clause is optional; all existing rules continue to work unchanged.

The `Filter` type lives in `tool/internal/filter/`; the YAML schema type `FilterDef` lives
in `tool/internal/rule/` alongside the other rule types. This keeps the import graph
one-directional (`filter` imports `rule`, not the reverse).

The accessor method is `GetWhere()` rather than `Where()` to follow the existing
`GetName / GetTarget / GetVersion` convention. Go does not allow a field and a method to
share the same name, so `Where()` would collide with the `Where *FilterDef` struct field.

## Consequences

- Combinators (`all-of`, `one-of`, `not`) and additional leaf types (`import_path`,
  `package_name`, `test_main`, `directive`) are stubbed and return descriptive errors until
  their respective follow-on PRs land:
  - `all-of`: [#381](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/381)
  - `one-of`: [#385](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/385)
  - `not`: [#386](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/386)
  - `import_path`: [#382](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/382)
  - `package_name`: [#383](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/383)
  - `test_main`: [#384](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/384)
- `Filter` implementations must be safe for concurrent use; they are evaluated from parallel
  goroutines in `matchDeps`.
- [#382](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/382) (`import_path` glob) requires a change to `matchDeps` indexing — see ADR-0004.
