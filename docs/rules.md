# Instrumentation Rules Documentation

## Table of Contents

- [Schema Reference](#schema-reference)
  - [Rule shape](#rule-shape)
  - [Top-level fields](#top-level-fields)
  - [Quick demo](#quick-demo)
  - [`where` semantics](#where-semantics)
  - [`where.file` semantics](#wherefile-semantics)
  - [`do` semantics](#do-semantics)
  - [Modifier names → rule types](#modifier-names--rule-types)
  - [Special `target` values](#special-target-values)
  - [Glob targets](#glob-targets)
  - [Valid and invalid shapes](#valid-and-invalid-shapes)
- [Loading Rules](#loading-rules)
  - [Rule Source Precedence](#rule-source-precedence)
- [Rule Types](#rule-types)
  - [1. Function Hook Rule](#1-function-hook-rule)
  - [2. Struct Field Injection Rule](#2-struct-field-injection-rule)
  - [3. Raw Code Injection Rule](#3-raw-code-injection-rule)
  - [4. Call Wrapping Rule](#4-call-wrapping-rule)
    - [Example 1: Wrapping Standard Library Calls](#example-1-wrapping-standard-library-calls)
    - [Example 2: Third-Party Library with Custom Alias](#example-2-third-party-library-with-custom-alias)
    - [Example 3: Using IIFE for Complex Wrapping with Deferred Cleanup](#example-3-using-iife-for-complex-wrapping-with-deferred-cleanup)
    - [Example 4: Appending gRPC Interceptors (non-ellipsis)](#example-4-appending-grpc-interceptors-non-ellipsis)
    - [Example 5: Appending gRPC Interceptors (ellipsis call)](#example-5-appending-grpc-interceptors-ellipsis-call)
  - [5. Directive Rule](#5-directive-rule)
  - [6. File Addition Rule](#6-file-addition-rule)
  - [7. Named Declaration Rule](#7-named-declaration-rule)

This document explains the different types of instrumentation rules used by the Go compile-time instrumentation tool. These rules, defined in YAML files, allow for the injection of code into target Go packages.

The schema is the 2-tier `target` / `version` + `where` / `do` surface decided in [ADR-0003](adr/0003-structured-rule-schema.md). `where` carries non-package selectors, `do` carries modifiers, and the modifier name in `do` declares the rule type.

Rules are typically distributed as `*.otelc.yml` files within instrumentation packages. Instrumentation packages may also contain an `otel.instrumentation.go` (or `otelc.tool.go`) file that composes other instrumentation packages through blank imports.

## Schema Reference

### Rule shape

Every rule is a YAML map entry whose key is the rule name:

```yaml
rule_name:
  target: <package import path>       # required
  version: <version range>            # optional
  where:                              # optional; non-package selectors
    <selector keys>
    file:
      <file predicate keys>
  do:                                 # required; modifier(s)
    - <modifier name>:
        <modifier keys>
  imports:                            # optional; injected imports
    <alias>: <path>
  name: <explicit name>               # optional; defaults to YAML key
```

### Top-level fields

| Key       | Required | Meaning                                                            |
| --------- | -------- | ------------------------------------------------------------------ |
| `target`  | yes      | Package import path or glob, matched against the `-p` flag.        |
| `version` | no       | Version range `start_inclusive,end_exclusive`. Omit to match all.  |
| `where`   | no       | Non-package selectors and file-level predicates.                   |
| `do`      | yes      | Ordered modifier list. Modifier name declares the rule type.       |
| `imports` | no       | `alias: path` map merged into instrumented files.                  |
| `name`    | no       | Explicit rule name; defaults to the YAML map key.                  |

Field notes:

- `target` (string, required): The import path of the Go package to be instrumented. For example, `golang.org/x/time/rate` or `main` for the main package. May also be a glob to match a package family — see [Glob targets](#glob-targets).
- `version` (string, optional): Specifies a version range for the target package using the format `start_inclusive,end_exclusive`. For example, `v0.11.0,v0.12.0` matches versions ≥ `v0.11.0` and < `v0.12.0`. Omit to match all versions.
- `where` (map, optional): Non-package selectors. Flat selector keys inside `where` are an implicit `all-of`. File-level predicates live under `where.file`. See [ADR-0003](adr/0003-structured-rule-schema.md#where-semantics) for the full list of selector keys and the qualifier composition (`all-of`, `one-of`, `not`).
- `do` (sequence, required): Ordered list of modifier entries. Each entry is a single-key map whose key names the modifier (`inject_hooks`, `inject_code`, `add_struct_fields`, `add_file`, `wrap_call`, `expand_directive`, `assign_value`). A single-modifier rule may also use map form (`do: <modifier>: …`), but the canonical form is the sequence form.
- `imports` (map[string]string, optional): A map of imports to inject into the instrumented file. The key is the import alias and the value is the import path. For standard imports without an alias, use the package name as both key and value. For blank imports, use `_` as the key. Function hook rules do not require this field — their imports are detected automatically from the hook source file.

  ```yaml
  imports:
    fmt: "fmt"       # Standard import: import "fmt"
    ctx: "context"   # Aliased import: import ctx "context"
    _: "unsafe"      # Blank import: import _ "unsafe"
  ```

### Quick demo

A single rule that instruments `(*sql.DB).Exec` — but only in files that also define an `init` function:

```yaml
instrument_sql_exec:
  target: database/sql
  where:
    func: Exec
    recv: "*DB"
    file:
      all-of:
        - has_func: init
  do:
    - inject_hooks:
        before: BeforeExec
        after: AfterExec
        path: github.com/example/sqlinstr
```

`target` pins the rule to the `database/sql` package. The `where` block narrows it further: `func` + `recv` together match only the `(*sql.DB).Exec` method. The `where.file` block adds a file-level gate — the hook is injected only into source files that also declare an `init` function (useful when setup code lives alongside `init`). Finally, `do` names the modifier (`inject_hooks`) and declares this as a Function Hook Rule; the tool will call `BeforeExec` at entry and `AfterExec` at exit, importing them from the given path automatically.

### `where` semantics

- Flat selector keys inside `where` are an implicit `all-of`.
- Composition sub-groups `all-of`, `one-of`, `not` may appear at any position
  to compose nested selector groups.
- Point selector keys recognized at the top of `where`:
  `func`, `recv`, `struct`, `function_call`, `directive`, `kind`,
  `identifier`.
- File-level predicates live under `where.file`.
- `target` and `version` **must not** appear inside `where`. They are
  package-scope selectors and stay top-level.

### `where.file` semantics

- Predicate keys: `has_func`, `has_recv`, `has_struct`, `has_directive`, `is_test`.
  Combinator keys: `all-of`, `one-of`, `not`.
- `has_recv` inside `where.file` narrows `has_func` to a specific receiver type.
- `is_test` is a tri-state boolean that gates on whether the file belongs to a
  test build — a compilation the Go toolchain produces only under `go test` (a
  package augmented with its `_test.go` files, an external `xxx_test` package,
  or the generated test-main runner). `is_test: true` matches only test builds,
  `is_test: false` only non-test builds, and omitting it applies no filter. It
  takes effect when building through `otelc go test`; a plain `otelc go build`
  never produces test builds. Production code in a package whose tests are all
  external (`package xxx_test`, no in-package `_test.go`) shares a single
  compile with normal builds, so `is_test` cannot gate that code.
- Exactly one leaf predicate must be active per `where.file` node;
  compositions are expressed via `all-of` / `one-of` / `not`.
- During the setup phase, leaf predicates (`has_func`, `has_recv`,
  `has_struct`, `is_test`) and the `where.file` combinators documented below are
  executed. `has_directive`, and combinators placed at the top level of
  `where` (outside `where.file`), are validated but return a descriptive
  "not yet supported" error at build time.

#### Combining `where.file` predicates

`all-of`, `one-of`, and `not` compose `where.file` predicates into boolean
expressions. A combinator **owns the node it appears on**: it cannot be mixed
with a sibling leaf predicate (`has_func`, `has_struct`, etc.) or another
combinator on the same node — that combination is **rejected at build time**
with a descriptive error, never silently ignored. Nest predicates inside the
combinator to express multiple conditions; combinators may be nested to any
depth. Presence is keyed on the YAML key, so an explicit empty list (for
example `all-of: []`) is a deliberate predicate, not an omission.

`all-of` matches when **every** nested predicate matches (logical AND); an
empty `all-of: []` matches vacuously (always true).

```yaml
# Instrument Connect only in the driver-registration file — the source file
# that declares both an `init` function and the `Driver` type.
register_driver:
  target: github.com/example/sqldriver
  where:
    func: Connect
    file:
      all-of:
        - has_func: init
        - has_struct: Driver
  do:
    - inject_hooks:
        before: BeforeConnect
        path: github.com/example/sqldriver/otel
```

`one-of` matches when **at least one** nested predicate matches (logical OR);
an empty `one-of: []` never matches (vacuously false).

```yaml
# Instrument Exec in the files that hold the driver's statement-execution
# code — those declaring either a `Conn` or a `Stmt` type.
trace_exec:
  target: github.com/example/sqldriver
  where:
    func: Exec
    file:
      one-of:
        - has_struct: Conn
        - has_struct: Stmt
  do:
    - inject_hooks:
        before: BeforeExec
        path: github.com/example/sqldriver/otel
```

`not` matches when its single nested predicate does **not** match (logical
negation). Unlike `all-of`/`one-of`, `not` is unary — it wraps exactly one
predicate, so there is no list and thus no empty-set case.

```yaml
# Instrument Connect everywhere except the in-memory mock file — the source
# file that declares a `MockConn` type, which must not be wrapped.
trace_connect:
  target: github.com/example/sqldriver
  where:
    func: Connect
    file:
      not:
        has_struct: MockConn
  do:
    - inject_hooks:
        before: BeforeConnect
        path: github.com/example/sqldriver/otel
```

### `do` semantics

`do` accepts two YAML shapes; both normalize to the same ordered internal list:

```yaml
# Sequence form — canonical, supports one or more modifiers.
do:
  - inject_hooks:
      before: BeforeOpen
      path: github.com/example/sql

# Map form — sugar for a single modifier.
do:
  inject_hooks:
    before: BeforeOpen
    path: github.com/example/sql
```

Rules:

- The sequence form is canonical and used in all in-repo examples.
- Each list item is a single-key map whose key names the modifier.
- Duplicate modifier kinds are allowed; declaration order is preserved.
- `do` must not be missing or empty.

### Modifier names → rule types

| Modifier            | Internal rule type  |
| ------------------- | ------------------- |
| `inject_hooks`      | `InstFuncRule`      |
| `inject_code`       | `InstRawRule`       |
| `add_struct_fields` | `InstStructRule`    |
| `add_file`          | `InstFileRule`      |
| `wrap_call`         | `InstCallRule`      |
| `expand_directive`  | `InstDirectiveRule` |
| `assign_value`      | `InstDeclRule`      |

**Planned:** rule type will be derived from the modifier key in `do`. The current implementation still infers from field presence and ignores the modifier name; see #546 for the planned migration.

### Special `target` values

- `target: main` — matches the compile-time package named `main`.
- `target: test_main` — not currently supported; reserved for future work.
- An empty or whitespace-only `target` is rejected at load time: `target` is
  the sole package selector, so a rule without one can never match.

### Glob targets

`target` accepts glob syntax so a single rule can instrument a whole package
family instead of one exact import path. A target is treated as a glob when it
contains any of `*`, `?`, `[`, or `{`; otherwise it stays an exact match and
keeps the fast map-lookup path.

Glob matching uses [`bmatcuk/doublestar`](https://github.com/bmatcuk/doublestar#patterns);
`/` is the segment delimiter:

| Pattern | Matches | Does **not** match |
| --- | --- | --- |
| `example.com/svc/*` | `example.com/svc/users` | `example.com/svc`, `example.com/svc/users/v2` |
| `example.com/svc/**` | `example.com/svc` and every descendant (`example.com/svc/users/v2`) | `example.com/other` |

- `*` matches within a single segment and never crosses `/`. `?`, `[...]`
  character classes, and `{alt1,alt2}` alternation also work per doublestar.
- `**` matches zero or more whole segments (`example.com/svc/**` matches both
  `example.com/svc` and `example.com/svc/users/v2`). Per doublestar, a `**`
  fused into a segment (`foo**`, `**bar`) is treated as a single-segment `*`.
- A malformed pattern (for example an unclosed `[`) is rejected at load time. A
  reversed range such as `[z-a]` is **not** an error; it simply never matches.

See the [doublestar pattern reference](https://github.com/bmatcuk/doublestar#patterns)
for the full grammar.

### Valid and invalid shapes

```yaml
# Minimal valid rule
open_hook:
  target: database/sql
  where:
    func: Open
  do:
    - inject_hooks:
        before: BeforeOpen
        path: github.com/example/sql

# File-level predicate — only in files that also declare an init function
open_hook_with_init:
  target: database/sql
  where:
    func: Open
    file:
      has_func: init
  do:
    - inject_hooks:
        before: BeforeOpen
        path: github.com/example/sql

# target inside where is rejected
invalid_target_in_where:
  where:
    target: net/http   # ERROR: target must be top-level
    func: Serve
  do:
    - inject_hooks:
        before: BeforeServe
        path: github.com/example/nethttp

# empty do is rejected
invalid_empty_do:
  target: net/http
  do: []             # ERROR: do must not be empty

# multi-key do item is rejected
invalid_multi_key_do_item:
  target: net/http
  do:
    - inject_hooks:
        before: BeforeServe
        path: github.com/example/nethttp
      inject_code:   # ERROR: each do item must be a single-key map
        raw: println("bad")

# scalar where.file is rejected
invalid_where_file_shape:
  target: net/http
  where:
    file: init       # ERROR: where.file must be a map
  do:
    - add_file:
        file: helpers.go
        path: github.com/example/helpers
```

---

## Loading Rules

Rules are normally distributed through instrumentation packages and enabled using `otel.instrumentation.go` (or `otelc.tool.go`) or `.otel.yml` config files.

For development and debugging, rules may also be loaded directly using the `--rules` (or `OTELC_RULES` environment variable) flag.

### Rule Source Precedence

When multiple rule sources are present, `otelc` resolves them using the following precedence order (highest to lowest):

1. `OTELC_RULES` environment variable
2. `--rules` flag
3. `otel.instrumentation.go` / `otelc.tool.go`
4. `.otel.yml`

Only the highest-precedence source that is present is used.

For example:

- If `OTELC_RULES` is set, all other rule sources are ignored.
- If `--rules` is provided, discovery through tool files and `.otel.yml`
  is skipped.
- If an `otel.instrumentation.go` (or `otelc.tool.go`) file exists, automatic
  rule discovery from `.otel.yml` is not used.
- `.otel.yml` is only considered when no higher-precedence rule source is
  available.

---

## Rule Types

There are several types of rules, each designed for a specific kind of code modification. Rule type is determined by the modifier name inside `do`.

### 1. Function Hook Rule

This is the most common rule type. It injects function calls at the beginning (`before`) and/or end (`after`) of a target function or method.

**Use Cases:**

- Wrapping functions with tracing spans.
- Adding logging statements to function entries and exits.
- Recording metrics about function calls.

**Selectors (under `where`):**

- `func` (string, required): The name of the target function to be instrumented.
- `recv` (string, optional): The receiver type for a method. For a standalone function, this field should be omitted. For a pointer receiver, it should be prefixed with `*`, e.g., `*MyStruct`.

**Modifier (`do: - inject_hooks:`):**

- `before` (string, optional): The name of the function to be called at the entry of the target function.
- `after` (string, optional): The name of the function to be called just before the target function returns.
- `path` (string, required): The import path for the package containing the `before` and `after` hook functions.
- `module` (string, optional): The module path where the hook functions are located. This is needed for built-in packages if import path is not a module root. Not required for external instrumentation packages.

**Example:**

```yaml
hook_helloworld:
  target: main
  where:
    func: Example
  do:
    - inject_hooks:
        before: MyHookBefore
        after: MyHookAfter
        path: "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/basic"
```

This rule will inject `MyHookBefore` at the start of the `Example` function in the `main` package, and `MyHookAfter` at the end. The hook functions are located in the specified `path`.

The tool automatically reads the hook source file and ensures all of its imports are present in the build. No `imports:` field is needed for function hook rules.

#### Signature Sub-Filters

By default the rule matches any function with the given name (and optional receiver). Five optional sub-filters, placed under `where` alongside `func`, can narrow the match further by inspecting the function's parameter and result types. All specified sub-filters must match (AND logic); omitting a sub-filter places no constraint on that aspect of the signature.

| Field | Type | Semantics |
| --- | --- | --- |
| `signature` | object | Exact match — the parameter list and result list must match the given type sequences in order |
| `signature_contains` | object | Partial match — at least one of the listed arg types appears anywhere in the parameter list **or** at least one of the listed return types appears anywhere in the result list |
| `result` | string | Any return type matches the named type |
| `last_result` | string | The last return type matches the named type |
| `param` | string | Any parameter type matches the named type |

`signature` and `signature_contains` each take an object with two optional lists:

- `args` — type names for parameters
- `returns` — type names for results

Type names follow the form `[*][pkg.]Name`, for example `error`, `context.Context`, `*http.Request`. Matching is structural (AST-level) rather than type-checker-based, so the package qualifier must match the local identifier used in the source file (typically the last path component, e.g. `http` for `"net/http"`).

> **Note on scalar filter semantics:** Because there is no type checker at instrumentation time, `result`, `last_result`, and `param` perform an exact type-name match. For example, `result: error` matches functions that literally return the `error` type, but not functions that return a concrete type (e.g. `*MyError`) that happens to implement `error`.
>
> **Unsupported type expressions:** Complex type expressions — `chan`, `func`, `map`, slice (`[]T`), and non-empty interface literals — cannot be matched by type-name filters. If a parameter or return value uses one of these forms, the filter will never match it.

**Example — match only functions with a specific signature:**

```yaml
hook_open:
  target: example.com/store
  where:
    func: Open
    signature:
      args: [context.Context, string]
      returns: ["*Connection", error]
  do:
    - inject_hooks:
        before: OnOpen
        path: example.com/hooks/store
```

The rule only applies when `Open` takes exactly a `context.Context` and a `string` and returns exactly a `*Connection` and an `error`. Functions named `Open` with different signatures are left untouched.

**Example — match any function that accepts a `context.Context`:**

```yaml
hook_ctx_funcs:
  target: example.com/worker
  where:
    func: Process
    signature_contains:
      args: [context.Context]
  do:
    - inject_hooks:
        before: OnProcess
        path: example.com/hooks/worker
```

**Example — match functions whose last return value is `error`:**

```yaml
hook_fallible:
  target: example.com/db
  where:
    func: Query
    last_result: error
  do:
    - inject_hooks:
        before: OnQuery
        path: example.com/hooks/db
```

### 2. Struct Field Injection Rule

This rule adds one or more new fields to a specified struct type.

**Use Cases:**

- Adding a context field to a struct to enable tracing through its methods.
- Extending existing data structures with new information without modifying the original source code.

**Selectors (under `where`):**

- `struct` (string, required): The name of the target struct.

**Modifier (`do: - add_struct_fields:`):**

- `new_field` (list of objects, required): A list of new fields to add to the struct. Each object in the list must contain:
  - `name` (string, required): The name of the new field.
  - `type` (string, required): The Go type of the new field.

**Example:**

```yaml
add_new_field:
  target: main
  where:
    struct: MyStruct
  do:
    - add_struct_fields:
        new_field:
          - name: NewField
            type: string
```

This rule adds a new field named `NewField` of type `string` to the `MyStruct` struct in the `main` package.

**Import Handling:**

If your new struct fields use types from external packages, specify those imports at the top level (`imports` is shared across selectors and modifiers):

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
    context: "context"
```

### 3. Raw Code Injection Rule

This rule injects a string of raw Go code at the beginning of a target function. This offers great flexibility but should be used with caution as the injected code is not checked for correctness at definition time.

**Use Cases:**

- Injecting complex logic that cannot be expressed with a simple function call.
- Quick and dirty debugging or logging.
- Prototyping new instrumentation strategies.
- Custom instrumentation for traces and metrics.

**Selectors (under `where`):**

- `func` (string, required): The name of the target function.
- `recv` (string, optional): The receiver type for a method.
- `pattern` (string, optional): The position within the function where the raw code should be injected. By default, the code is injected at the start of the function body. If provided, the value must be a valid regular expression.

  The pattern is matched against the canonical gofmt representation for each statement in the function body (not always the exact original source formatting). The injected code is placed immediately before/after the first statement that matches the pattern.

  If no statement matches the pattern, an error is returned.

- `placement` (string, optional): Determines where to inject the raw code when a `pattern` is specified. Can be either `before` (default) or `after`.

**Modifier (`do: - inject_code:`):**

- `raw` (string, required): The raw Go code to be injected. The code will be inserted at the beginning of the target function.

Top-level `imports` (map[string]string, optional): A map of imports to inject into the target file. Required when the injected code references packages not already imported by the target. Same format as [Top-level fields](#top-level-fields).

**Example:**

```yaml
raw_helloworld:
  target: main
  where:
    func: Example
  do:
    - inject_code:
        raw: 'go func(){ println("RawCode") }()'
```

This rule injects a new goroutine that prints "RawCode" at the start of the `Example` function in the `main` package.

**Example with imports:**

Raw code frequently references packages that the target file does not already import. Use the top-level `imports:` field to inject those declarations:

```yaml
raw_with_hash:
  target: main
  where:
    func: Example
  do:
    - inject_code:
        raw: |
          go func(){
            h := sha256.New()
            h.Write([]byte("RawCode"))
            fmt.Printf("RawCode: %x\n", h.Sum(nil))
          }()
  imports:
    fmt: "fmt"
    sha256: "crypto/sha256"
```

**Example with pattern:**

Sometimes you may want to inject raw code at a specific location within the function body rather than at the start. Use the `pattern` field with a regex pattern to specify the injection point:

```yaml
raw_with_pattern:
  target: main
  where:
    func: Example
    pattern: '^println\\("hello"\\)$'
  do:
    - inject_code:
        raw: 'go func(){ println("RawCode") }()'
```

If `Example()` looks like this:

```go
func Example() {
  if true {
    println("hello")
  }
}
```

The injected code will be placed immediately before the `println("hello")` statement.

Note the pattern starts with `^`. During AST traversal, outer statements (such as `if`, `for`, or `go func`) are visited before their inner statements. Since matching is performed on the formatted string of each statement, a loose pattern may accidentally match a parent statement if it contains the target code.

Anchoring the pattern with `^` (and usually `$`) ensures that only the exact statement is matched, preventing insertion at the wrong level (e.g., before an entire block instead of the intended inner statement).

### 4. Call Wrapping Rule

This rule wraps function calls at call sites with instrumentation code. Unlike the Function Hook Rule which instruments function definitions, this rule instruments where functions are called.

**Use Cases:**

- Wrapping HTTP client calls with tracing.
- Adding context to database query calls.
- Monitoring third-party library calls without modifying the library.
- Call-site specific instrumentation (different behavior per call location).

**Selectors (under `where`):**

| Field           | Type   | Required | Notes                                                |
| --------------- | ------ | -------- | ---------------------------------------------------- |
| `function_call` | string | Yes      | Qualified function name: `package/path.FunctionName` |

**Modifier (`do: - wrap_call:`):**

| Field           | Type       | Required                                      | Notes                                                                                                                  |
| --------------- | ---------- | --------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `replace`       | string     | No (one of `replace`/`append_args` required)  | Replace string with `{{ . }}` placeholder for the original call. Must produce a Go call expression.                    |
| `append_args`   | `[]string` | No (one of `replace`/`append_args` required)  | Go expression strings appended as additional arguments to the matched call                                             |
| `variadic_type` | string     | No                                            | Element type for the ellipsis IIFE wrapper (e.g. `grpc.DialOption`). Required when any matched call uses `...` spread. |

Top-level `imports` (map[string]string, optional): Additional imports needed for injected code (alias: path). Packages must be in the target module's `go.mod`.

**`replace` and `append_args` are independent and can both be set.** When both are present, `append_args` is applied first (arguments are appended to the call), then `replace` wraps the modified call.

**`replace` String System:**

The `replace` field uses Go's standard `text/template` package for code generation. This provides:

- **Placeholder Substitution**: `{{ . }}` is replaced with the original function call's AST node
- **Type Safety**: The replace string is compiled at rule creation time and validated
- **Expression Output**: The replace string must produce a valid Go expression; the result may be any expression type (not limited to call expressions)

Currently supported replace string features:

- Simple wrapping: `wrapper({{ . }})`
- IIFE (Immediately-Invoked Function Expression): `(func() T { return {{ . }} })()`
- Complex expressions with multiple statements using IIFE

**`append_args` Semantics:**

The `append_args` field appends one or more Go expressions as additional arguments to each matched call.

- **Non-ellipsis calls** (e.g. `grpc.NewClient(addr, opt1)`): the new expressions are appended directly — `grpc.NewClient(addr, opt1, newArg)`.
- **Ellipsis calls** (e.g. `grpc.Dial(addr, opts...)`): `variadic_type` must be set. An IIFE wrapper is generated that appends the new args to the spread argument:

```go
// Before:
grpc.Dial(addr, opts...)

// After (variadic_type: "grpc.DialOption"):
grpc.Dial(addr, func(v ...grpc.DialOption) []grpc.DialOption {
    return append(v, grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()))
}(opts...)...)
```

If a matched call uses `...` and `variadic_type` is not set, the call is **skipped** with a logged warning.

**Understanding function_call Matching:**

The `function_call` field must use the qualified format: `package/path.FunctionName`

Examples:

- `net/http.Get` matches `http.Get()` where `http` is imported from `"net/http"`
- `github.com/redis/go-redis/v9.Get` matches `redis.Get()` from that package
- `database/sql.Open` matches `sql.Open()` calls

**What does NOT match:**

- Unqualified calls like `Get()` without a package prefix
- Calls from different packages (e.g., `other.Get()` when rule specifies `net/http.Get`)

**Examples:**

#### Example 1: Wrapping Standard Library Calls

```yaml
wrap_http_get:
  target: myapp/server
  where:
    function_call: net/http.Get
  do:
    - wrap_call:
        replace: "tracedGet({{ . }})"
```

In the `myapp/server` package, this transforms:

```go
import "net/http"

func fetchData(url string) {
    resp, err := http.Get(url)  // Original call
    // becomes:
    resp, err := tracedGet(http.Get(url))  // Wrapped call
}
```

**Note:** The `tracedGet` function must be available in the target package, either defined locally or imported.

**What gets wrapped:** Only `http.Get()` calls where `http` is imported from `"net/http"`

**What does NOT get wrapped:**

- `Get()` calls without the `http.` qualifier
- `other.Get()` calls where `other` is a different package

---

#### Example 2: Third-Party Library with Custom Alias

```yaml
wrap_redis_get:
  target: myapp/cache
  where:
    function_call: github.com/redis/go-redis/v9.Get
  do:
    - wrap_call:
        replace: "tracedRedisGet(ctx, {{ . }})"
```

In the `myapp/cache` package:

```go
import redis "github.com/redis/go-redis/v9"

func getValue(ctx context.Context, key string) {
    val, err := redis.Get(ctx, key)  // Original
    // becomes:
    val, err := tracedRedisGet(ctx, redis.Get(ctx, key))  // Wrapped
}
```

**Note:** The `tracedRedisGet` function must be available in the target package.

---

#### Example 3: Using IIFE for Complex Wrapping with Deferred Cleanup

This example demonstrates the power of the `replace` string by using an IIFE (Immediately-Invoked Function Expression) to wrap a call with complex logic:

```yaml
wrap_with_unsafe:
  target: client
  where:
    function_call: myapp/utils.Helper
  do:
    - wrap_call:
        replace: "(func() (float32, error) { r, e := {{ . }}; _ = unsafe.Sizeof(r); return r, e })()"
```

This uses an immediately-invoked function expression (IIFE) to inject logic after the call:

```go
package client

import (
    "unsafe"
    utils "myapp/utils"
)

func process() {
    result, err := utils.Helper("test", 42)
    // becomes:
    result, err := (func() (float32, error) {
        r, e := utils.Helper("test", 42)
        _ = unsafe.Sizeof(r)  // Additional logic
        return r, e
    })()
}
```

**Note:** The `unsafe` package must be imported in the target file for this replace string to work.

---

#### Example 4: Appending gRPC Interceptors (non-ellipsis)

```yaml
add_otel_grpc_interceptors:
  target: myapp
  where:
    function_call: google.golang.org/grpc.NewClient
  do:
    - wrap_call:
        append_args:
          - "grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor())"
          - "grpc.WithStreamInterceptor(otelgrpc.StreamClientInterceptor())"
  imports:
    grpc: "google.golang.org/grpc"
    otelgrpc: "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
```

This transforms `grpc.NewClient("localhost:50051")` into:

```go
grpc.NewClient("localhost:50051",
    grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),
    grpc.WithStreamInterceptor(otelgrpc.StreamClientInterceptor()))
```

---

#### Example 5: Appending gRPC Interceptors (ellipsis call)

When the call site uses a spread (`opts...`), set `variadic_type`:

```yaml
add_otel_grpc_dial_interceptors:
  target: myapp
  where:
    function_call: google.golang.org/grpc.Dial
  do:
    - wrap_call:
        append_args:
          - "grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor())"
        variadic_type: "grpc.DialOption"
  imports:
    grpc: "google.golang.org/grpc"
    otelgrpc: "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
```

This transforms `grpc.Dial(addr, opts...)` into:

```go
grpc.Dial(addr, func(v ...grpc.DialOption) []grpc.DialOption {
    return append(v, grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()))
}(opts...)...)
```

---

**Important Notes:**

- The `{{ . }}` placeholder in the `replace` string represents the original function call.
- The `replace` string must be a valid Go expression that includes the placeholder and produces a call expression (current limitation).
- The `replace` string can only reference packages and functions that are already imported or defined in the target file.
- Call rules only affect call sites in the target package, not the function definition itself.
- Multiple calls to the same function will all be wrapped independently.
- Use the qualified format `package/path.FunctionName` for functions.
- All packages referenced in `append_args` must be in the target module's `go.mod`.
- Ellipsis calls without `variadic_type` are skipped with a logged warning.

### 5. Directive Rule

This rule instruments functions annotated with a magic comment (a "directive") by prepending templated Go code into their bodies. The template is rendered once per annotated function, and the resulting statements are inserted at the top of the function body.

**Use Cases:**

- Automatically injecting tracing spans into functions the author has opted into with a comment.
- Adding logging or metrics boilerplate that developers annotate functions with.
- Any "opt-in" instrumentation where the annotation lives in source code rather than a rule file.

**Fields:**

**Selectors (under `where`):**

- `directive` (string, required): The directive name to match, without the leading `//`. Must not contain spaces. For example, `otelc:span` matches the comment `//otelc:span`. Note that a space after `//` (e.g., `// otelc:span`) does **not** match — the directive must immediately follow `//`.

**Modifier (`do: - expand_directive:`):**

- `template` (string, required): Go statements to prepend to each matching function body. Rendered with [fasttemplate](https://github.com/valyala/fasttemplate) using `{{` / `}}` delimiters. The only supported placeholder is `{{FuncName}}`, which is replaced with the name of the annotated function.

Top-level `imports` (map[string]string, optional): Additional imports needed by the injected code. Same format as [Top-level fields](#top-level-fields).

**Template Placeholders:**

| Placeholder    | Replaced with                      |
| -------------- | ---------------------------------- |
| `{{FuncName}}` | The name of the annotated function |

**Example:**

```yaml
span_directive:
  target: main
  where:
    directive: "otelc:span"
  do:
    - expand_directive:
        replace: |-
          println("span start: {{FuncName}}")
          defer println("span end: {{FuncName}}")
```

Given this source file:

```go
//otelc:span
func foo() {
    println("hello")
}
```

The instrumented output becomes:

```go
//otelc:span
func foo() {
    println("span start: foo")
    defer println("span end: foo")
    println("hello")
}
```

**Important Notes:**

- The directive comment must be placed immediately before the function declaration.
- The `//` must not be followed by a space (i.e., `//otelc:span`, not `// otelc:span`).
- The `directive` field must not include the leading `//`.
- Functions without the directive comment are not affected.
- Multiple functions in the same file can carry the directive; each gets the template applied independently with its own `{{FuncName}}`.

### 6. File Addition Rule

This rule adds a new Go source file to the target package.

**Use Cases:**

- Adding new helper functions required by other hooks.
- Introducing new functionalities or APIs to an existing package.

**Selectors:** none. File rules apply to the target package as a whole and have no point selector.

**Modifier (`do: - add_file:`):**

- `file` (string, required): The name of the new file to be added (e.g., `newfile.go`).
- `path` (string, required): The import path of the package where the content of the new file is located. The instrumentation tool will find the file within this package.

  The package referenced by `path` must be importable by `go/packages`. If the implementation files are marked with `//go:build ignore` (for example because they rely on otelc compile-time transformations), include a small buildable stub file so the package remains importable during rule resolution.
- `module` (string, optional): The module path where the file is located. This is needed for built-in packages if import path is not a module root. Not required for external instrumentation packages.

**Example:**

```yaml
add_new_file:
  target: main
  do:
    - add_file:
        file: "new_helpers.go"
        path: "github.com/my-org/my-repo/instrumentation/helpers"
```

This rule would take the file `new_helpers.go` from the `github.com/my-org/my-repo/instrumentation/helpers` package and add it to the `main` package during compilation.

**Import Handling:**

File rules typically don't need the `imports` field because the added file already contains its own import declarations. However, you can use the top-level `imports:` to add additional imports to the file being added:

```yaml
add_file_with_extra_imports:
  target: main
  do:
    - add_file:
        file: "helpers.go"
        path: "github.com/my-org/my-repo/instrumentation/helpers"
  imports:
    log: "log" # Add extra import to the copied file
```

### 7. Named Declaration Rule

This rule targets a named package-level symbol (variable, constant, function, or type) and replaces or wraps its initializer. It is the primary mechanism for overriding or decorating default values in third-party packages without modifying their source — for example, replacing a default HTTP transport with an instrumented one, or wrapping an existing transport to inject OTel tracing.

**Use Cases:**

- Replacing a package-level `var` with an instrumented implementation (e.g., `http.DefaultTransport`).
- Wrapping an existing package-level `var` initializer with an OTel instrumentation layer.
- Toggling a package-level flag or sentinel value for observability purposes.
- Substituting a registered implementation at compile time.

**Selectors (under `where`):**

- `kind` (string, optional): Constrains the kind of symbol to match. Valid values: `var`, `const`, or omitted/empty to match any kind. (`func` and `type` are recognized but not currently supported — no action can be applied to them.)
- `identifier` (string, required): The name of the top-level symbol to match.

**Modifier (`do: - assign_value:`):**

- `replace` (string, optional): A Go expression to assign as the new value of the matched `var` or `const`. Mutually exclusive with `wrap`. Not valid when `kind` is `func` or `type`.
- `wrap` (string, optional): A Go expression template that wraps the existing initializer of the matched `var` or `const`. `{{ . }}` is substituted with the original expression. Mutually exclusive with `replace`. Not valid when `kind` is `func` or `type`.

Top-level `imports` (map[string]string, optional): Additional imports needed by the injected expression. Same format as [Top-level fields](#top-level-fields).

> **Note:** Exactly one of `replace` or `wrap` must be set.

**Example (replace):**

```yaml
assign_default_transport:
  target: net/http
  where:
    kind: var
    identifier: DefaultTransport
  do:
    - assign_value:
        replace: |
          &http.Transport{
            MaxIdleConns:    100,
            MaxConnsPerHost: 100,
          }
  imports:
    http: "net/http"
```

This rule replaces `http.DefaultTransport` in the `net/http` package with a custom `*http.Transport` at compile time, enabling all outbound HTTP calls to use the configured transport — a common pattern for injecting tracing or connection-pool tuning without modifying the standard library source.

**Example (wrap):**

```yaml
wrap_default_transport:
  target: net/http
  where:
    kind: var
    identifier: DefaultTransport
  do:
    - assign_value:
        wrap: "otelhttp.NewTransport({{ . }})"
  imports:
    otelhttp: "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
```

This rule wraps the existing `http.DefaultTransport` value with `otelhttp.NewTransport`, injecting OTel tracing into all outbound HTTP calls without replacing the transport configuration.

**Notes:**

- `replace` must be a valid Go expression (not a statement).
- `wrap` must contain `{{ . }}` as a placeholder for the original expression. The template must produce exactly one expression.
- `wrap` returns an error at instrumentation time if the matched declaration has no initializer (e.g., `var X T` without `= ...`).
- If `replace` matches multiple names in a single declaration (e.g., `var a, b = ...`), the replacement expression is cloned and assigned to each name.
- If `wrap` matches multiple initialized values in a single declaration, each initializer is wrapped independently.
- Omitting `kind` matches the first symbol with the given name regardless of kind.
