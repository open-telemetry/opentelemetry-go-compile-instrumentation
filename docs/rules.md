# Instrumentation Rules

Rules are YAML documents that tell the compile-time instrumentation tool:

- which package to inspect
- which symbol or file shape to match inside that package
- which modification to apply

The canonical schema is the 2-tier `target` / `version` + `where` / `do` surface described in [ADR 0003](adr/0003-structured-rule-schema.md).

## Rule Shape

```yaml
rule_name:
  target: database/sql
  version: "v1.0.0,v2.0.0" # optional
  where:
    func: Open
    file:
      has_func: init
  do:
    - inject_hooks:
        before: BeforeOpen
        after: AfterOpen
        path: github.com/example/sql
  imports:
    fmt: fmt
```

Top-level fields:

- `target` (required): package selector. Usually an import path such as `database/sql`.
- `version` (optional): version range in `start,end` form. The left bound is inclusive and the right bound is exclusive.
- `where` (optional): all non-package selectors.
- `do` (required): ordered list of modifier entries.
- `imports` (optional): additional imports needed by the modifier payload.

## Selector Semantics

### Package selectors

`target` and `version` are special top-level selectors because package matching happens before symbol or file matching.

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
        path: github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/grpc/server
```

### `where`

`where` contains every selector other than package and version matching.

Supported selector fields:

- `func`
- `recv`
- `struct`
- `function_call`
- `directive`
- `kind`
- `identifier`
- `file`

Flat fields inside `where` are implicit `all-of`.

Example:

```yaml
method_hook:
  target: net/http
  where:
    func: ServeHTTP
    recv: "*Server"
  do:
    - inject_hooks:
        before: BeforeServeHTTP
        after: AfterServeHTTP
        path: github.com/example/nethttp
```

This matches only when both selectors match.

### Qualifier composition

Explicit qualifier keys remain part of the schema:

- `all-of`
- `one-of`
- `not`

Example:

```yaml
open_variants:
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

The parser accepts and preserves this surface. Broader execution of qualifier composition is follow-up work; this PR only normalizes the agreed schema.

### File predicates

File-level predicates live under `where.file`.

Example:

```yaml
only_if_file_has_init:
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

`where.file` supports implicit `all-of` plus explicit qualifier keys in the schema. The currently executed file predicates are intentionally narrower than the full schema surface.

## Modifier Semantics

`do` is an ordered YAML sequence. Each item must be a single-key typed modifier object.

Rules:

- `do` must not be missing
- `do` must not be empty
- each item must contain exactly one modifier key
- duplicate modifier kinds are allowed
- declaration order is preserved

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
  imports:
    context: context
```

## Target Edge Cases

### Wildcard target

```yaml
wildcard_main:
  target: "*"
  where:
    func: main
  do:
    - inject_hooks:
        before: BeforeMain
        path: github.com/example/shared
```

### `target: main`

`main` is valid today. The setup phase already indexes rules by the compile-time package import path, and `main` participates in that flow naturally.

### `target: test_main`

`test_main` is not a supported special target today. The dependency model does not currently carry a package-kind or test-binary discriminator, so that target needs separate discovery work.

## Rule Types

| Modifier | Primary selectors in `where` | Purpose |
| --- | --- | --- |
| `inject_hooks` | `func`, optional `recv` | insert before/after hooks around a function or method |
| `inject_code` | `func`, optional `recv` | inject raw Go statements at function entry |
| `add_struct_fields` | `struct` | add fields to a struct definition |
| `add_file` | optional `file` predicate only | copy a Go file into the target package |
| `wrap_call` | `function_call` | wrap matching call sites with a template |
| `expand_directive` | `directive` | expand a magic comment into statements |
| `assign_value` | `kind`, `identifier` | replace the initializer of a package-level symbol |

### Function Hook Rule

```yaml
hook_serve_http:
  target: net/http
  where:
    func: ServeHTTP
    recv: serverHandler
  do:
    - inject_hooks:
        before: BeforeServeHTTP
        after: AfterServeHTTP
        path: github.com/example/nethttp/server
```

Modifier fields:

- `before` (optional)
- `after` (optional)
- `path` (required)

### Raw Code Injection Rule

```yaml
raw_debug:
  target: main
  where:
    func: Example
  do:
    - inject_code:
        raw: |
          go func() {
            println("RawCode")
          }()
```

Modifier fields:

- `raw` (required)

### Struct Field Injection Rule

```yaml
add_context_field:
  target: main
  where:
    struct: MyStruct
  do:
    - add_struct_fields:
        new_field:
          - name: ctx
            type: context.Context
  imports:
    context: context
```

Modifier fields:

- `new_field` (required list of `{name, type}`)

### Call Wrapping Rule

```yaml
wrap_http_get:
  target: myapp/server
  where:
    function_call: net/http.Get
  do:
    - wrap_call:
        template: tracedGet({{ . }})
```

Modifier fields:

- `template` (required)

### Directive Rule

```yaml
span_directive:
  target: main
  where:
    directive: otelc:span
  do:
    - expand_directive:
        template: |-
          println("span start: {{FuncName}}")
          defer println("span end: {{FuncName}}")
```

Modifier fields:

- `template` (required)

### File Addition Rule

```yaml
add_helpers:
  target: main
  where:
    file:
      has_func: init
  do:
    - add_file:
        file: helpers.go
        path: github.com/example/helpers
```

Modifier fields:

- `file` (required)
- `path` (required)

### Named Declaration Rule

```yaml
assign_default_transport:
  target: net/http
  where:
    kind: var
    identifier: DefaultTransport
  do:
    - assign_value:
        value: |
          &http.Transport{
            MaxIdleConns: 100,
          }
  imports:
    http: net/http
```

Modifier fields:

- `value` (required Go expression)

## Invalid Shapes

These are rejected during parsing or validation:

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
invalid_where_file:
  target: net/http
  where:
    file: init
  do:
    - add_file:
        file: helpers.go
        path: github.com/example/helpers
```
