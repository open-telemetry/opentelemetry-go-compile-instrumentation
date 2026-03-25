# 3. Join Point Filter Interface

Date: 2026-03-19 (revised 2026-03-25)

## Status

Accepted

## Context

The tool supports flat, single-predicate rules: one `func`, `struct`, `call`, `raw`, or
`file` field per rule. There is no way to compose multiple conditions — for example,
"instrument functions annotated with `//otelc:span`" — or to filter on which source files
within a target package a rule applies to.

Orchestrion (DataDog's equivalent tool) uses an Aspect-Oriented Programming model with a
three-phase `Point` interface: `PackageMayMatch`, `FileMayMatch`, and `Matches`. The first
two phases allow cheap early-exit before source files are parsed.

## Decision

### 3-Tier Granularity Model

Rules are evaluated in three tiers of increasing specificity:

```
Tier 1 — Package Scope:   target (exact or glob) + version
Tier 2 — File Predicate:  where clause (FilterDef)
Tier 3 — Point Selector:  rule-type fields (func, struct, directive, function_call, …)
```

**Tier 1** identifies which packages are candidates. `matchDeps` filters by `target`
(exact or glob) and `version`.

**Tier 2** decides which source files within the package are processed. The optional
`where` clause holds a `FilterDef` (YAML) that is compiled into a `Filter` interface once
per rule before source-file iteration in `preciseMatching`, and evaluated per source file.

**Tier 3** finds the exact declaration to instrument. This is the existing AST matching
logic (`FindFuncDecl`, `FindStructDecl`, etc.).

### The `has_` Prefix Convention

Both Tier 2 and Tier 3 refer to functions, structs, and directives, which would create
ambiguous YAML keys if the same names were used. The `has_` prefix on Tier 2 predicates
makes the distinction unambiguous:

| Level   | Example YAML         | Meaning                                         |
|---------|----------------------|-------------------------------------------------|
| Tier 3  | `func: Handler`      | Instrument the function named `Handler`         |
| Tier 2  | `has_func: init`     | Only in files that contain a function `init`    |
| Tier 3  | `struct: Server`     | Instrument the struct named `Server`            |
| Tier 2  | `has_struct: Server` | Only in files that declare a struct `Server`    |

### FilterDef Schema

`FilterDef` is the YAML representation of a Tier 2 predicate:

```go
type FilterDef struct {
    // Combinators — not yet implemented.
    AllOf []FilterDef
    OneOf []FilterDef
    Not   *FilterDef

    // Leaf file predicates.
    HasFunc      string // file contains a function with this name
    HasRecv      string // optional modifier for HasFunc: function has this receiver type
    HasStruct    string // file contains a struct with this name
    HasDirective string // file contains this directive comment (not yet implemented)
    IncludeTest  *bool  // package is a test compilation unit (not yet implemented)
}
```

`HasRecv` is only meaningful alongside `HasFunc`; it narrows the match to methods on a
specific receiver type. All other predicates are independent.

A concrete three-tier example:

```yaml
instrument-handler:
  target: "github.com/myapp/server"   # Tier 1: exact package scope
  version: "v1.0.0,v2.0.0"           # Tier 1: version range
  func: Handler                       # Tier 3: point selector
  before: BeforeHandler
  where:                              # Tier 2: file predicate
    has_struct: Server
```

This rule instruments `Handler` only in files within `github.com/myapp/server` that also
declare the `Server` struct. Files without `Server` are skipped entirely.

### Filter Interface

```go
type Filter interface {
    Match(ctx *MatchContext) bool
}
```

`MatchContext` carries import path, source file path, and the parsed AST. Filters are
built once per rule via `filter.Build()` and evaluated once per source file — not once per
invocation. The `where` clause is optional; all existing rules continue to work unchanged.

The `Filter` type lives in `tool/internal/filter/`; the YAML schema type `FilterDef` lives
in `tool/internal/rule/` alongside the other rule types. This keeps the import graph
one-directional (`filter` imports `rule`, not the reverse).

The accessor method is `GetWhere()` rather than `Where()` to follow the existing
`GetName / GetTarget / GetVersion` convention (a field and method cannot share a name in
Go).

## Consequences

- Combinators (`all-of`, `one-of`, `not`) and additional leaf types (`has_directive`,
  `include_test`) are stubbed and return descriptive errors until their respective
  follow-on PRs land:
  - `all-of`: [#381](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/381)
  - `one-of`: [#385](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/385)
  - `not`: [#386](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/386)
  - `has_directive` / `include_test`: future PRs
- `Filter` implementations must be safe for concurrent use; they are evaluated from
  parallel goroutines in `matchDeps`.
- Target glob support (Tier 1) is documented in ADR-0004 and implemented in a follow-on
  PR that replaces the now-closed #382.
