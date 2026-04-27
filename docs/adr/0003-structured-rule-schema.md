# 3. Structured Rule Schema (Option 2)

Date: 2026-03-26

## Status

Accepted

## Context

Rule YAML defines two different concerns:

- package selection: which package and version to instrument
- rule selection and modification: which symbols or files inside that package to match, and what to do when they match

The previous flat schema mixed selectors and modifier fields at one level. The first structured proposal also treated every selector the same by moving `target` and `version` under `where`. That shape made package selection harder to scan and blurred an important distinction in the setup pipeline: package matching happens before symbol or file matching.

## Decision

Adopt a 2-tier schema with two special top-level package selectors:

- `target`
- `version`

All other selectors live under `where`. All modifiers live under `do`.

Canonical shape:

```yaml
hook_open:
  target: database/sql
  version: "v1.0.0,v2.0.0"
  where:
    func: Open
    file:
      has_func: init
  do:
    - inject_hooks:
        before: beforeOpen
        after: afterOpen
        path: github.com/example/instrumentation/sql
    - something_else:
        xxxx: yyyy
```

### Top-level fields

| Key | Meaning |
| --- | --- |
| `target` | package selector; import path or special package target |
| `version` | optional package version range; lower bound inclusive, upper bound exclusive |
| `where` | all non-package selectors |
| `do` | ordered YAML sequence of typed modifier objects |
| `imports` | optional imports used by the modifier payload |
| `name` | optional explicit name; the YAML key remains the canonical rule identifier |

### `where` semantics

- Flat selector fields inside `where` are implicit `all-of`.
- Explicit qualifier-based composition remains available through `all-of`, `one-of`, and `not`.
- Point selectors such as `func`, `recv`, `struct`, `function_call`, `directive`, `kind`, and `identifier` live directly under `where`.
- File predicates live under `where.file`.

Examples:

```yaml
exact_package_match:
  target: net/http
  where:
    func: Serve
  do:
    - inject_hooks:
        before: BeforeServe
        path: github.com/example/nethttp
```

```yaml
version_bounded_rule:
  target: google.golang.org/grpc
  version: "v1.63.0,v1.70.0"
  where:
    func: NewServer
  do:
    - inject_hooks:
        before: BeforeNewServer
        after: AfterNewServer
        path: github.com/example/grpc/server
```

```yaml
file_predicate:
  target: main
  where:
    func: Run
    file:
      has_func: init
  do:
    - inject_hooks:
        before: BeforeRun
        path: github.com/example/main
```

```yaml
selector_composition:
  target: database/sql
  where:
    one-of:
      - func: Open
      - func: OpenDB
    not:
      recv: "*DB"
  do:
    - inject_hooks:
        before: BeforeOpen
        path: github.com/example/sql
```

### `do` semantics

- `do` is a YAML sequence.
- Each item is a single-key typed modifier object.
- Multiple modifier entries are allowed.
- Duplicate modifier kinds are allowed.
- Application order is declaration order.

Example:

```yaml
multiple_modifiers:
  target: runtime
  where:
    struct: g
  do:
    - add_struct_fields:
        new_field:
          - name: traceCtx
            type: context.Context
    - add_struct_fields:
        new_field:
          - name: spanID
            type: string
```

### Modifier names and rule types

| Modifier | Internal rule type |
| --- | --- |
| `inject_hooks` | `InstFuncRule` |
| `inject_code` | `InstRawRule` |
| `add_struct_fields` | `InstStructRule` |
| `add_file` | `InstFileRule` |
| `wrap_call` | `InstCallRule` |
| `expand_directive` | `InstDirectiveRule` |
| `assign_value` | `InstDeclRule` |

Rule type inference comes from the modifier object, not from a priority order over selector fields.

## Special `target` Semantics

### Wildcards

`target` remains the package-level selector. Wildcards are still valid because matching is performed against the dependency import path.

Example:

```yaml
match_all_main_packages:
  target: "*"
  where:
    func: main
  do:
    - inject_hooks:
        before: BeforeMain
        path: github.com/example/shared
```

### `target: main`

`target: main` is viable with the current setup pipeline.

Why:

- dependency discovery already captures the `-p` package argument from the compile command as `dep.ImportPath`
- rule matching indexes rules by `dep.ImportPath`
- the `main` package fits that model directly

No extra dependency classification is required.

### `target: test_main`

`target: test_main` is not directly viable with the current plumbing.

Why:

- `Dependency` currently carries `ImportPath`, `Version`, `Sources`, and `CgoFiles`
- current matching is keyed by `rulesByTarget[dep.ImportPath]`
- there is no existing package-kind or test-binary discriminator in the dependency model

Supporting `test_main` would require new discovery or classification logic. It is a follow-up capability, not part of this schema decision.

## MxN Interpretation

This ADR does not describe MxN as "per-modifier selectors".

The intended reading is:

- `where` can express multiple selector matches through implicit `all-of` and explicit qualifiers such as `one-of` and `not`
- `do` can contain multiple modifier entries

That is the schema surface. How much of that surface is executed by the current matcher can evolve independently.

## Invalid Shapes

These shapes are invalid:

```yaml
invalid_target_in_where:
  where:
    target: net/http
    func: Serve
  do:
    - inject_hooks:
        before: BeforeServe
        path: github.com/example/nethttp
```

```yaml
invalid_empty_do:
  target: net/http
  where:
    func: Serve
  do: []
```

```yaml
invalid_multi_key_do_item:
  target: net/http
  where:
    func: Serve
  do:
    - inject_hooks:
        before: BeforeServe
        path: github.com/example/nethttp
      inject_code:
        raw: println("bad")
```

```yaml
invalid_where_file_shape:
  target: net/http
  where:
    file: init
  do:
    - add_file:
        file: helpers.go
        path: github.com/example/helpers
```

## Consequences

Positive:

- package selection stays obvious at the top of every rule
- selector composition and modifier sequencing both have an explicit home
- the schema supports future qualifier expansion without requiring a parallel document

Tradeoffs:

- YAML is more explicit than the old flat format
- the parser must normalize the schema before creating internal rule structs
- this ADR settles the semantic surface ahead of the full internal execution refactor

## Implementation Note

`#395` intentionally implements only the agreed syntax surface and the minimum normalization needed to map that surface back into the current internal model.

In particular:

- `target` and `version` stay top-level
- `where.file` is preserved for runtime evaluation
- `do` is normalized into ordered internal rule entries
- broader qualifier execution remains follow-up work
