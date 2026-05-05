# Instrumentation Rules Documentation

This document explains the different types of instrumentation rules used by the Go compile-time instrumentation tool. These rules, defined in YAML files, allow for the injection of code into target Go packages.

## Common Fields

All rules share a set of common fields that define the target of the instrumentation.

- `target` (string, required): The import path of the Go package to be instrumented. For example, `golang.org/x/time/rate` or `main` for the main package.
- `version` (string, optional): Specifies a version range for the target package. The rule will only be applied if the package's version falls within this range. The format is `start_inclusive,end_exclusive`. For example, `v0.11.0,v0.12.0` means the rule applies to versions greater than or equal to `v0.11.0` and less than `v0.12.0`. If omitted, the rule applies to all versions.
- `imports` (map[string]string, optional): A map of imports to inject into the instrumented file. The key is the import alias and the value is the import path. For standard imports without an alias, use the package name as both key and value. For blank imports, use `_` as the key. This field is used by raw, struct, and call rules. Function hook rules do not require it — their imports are detected automatically from the hook source file.

  Examples:

  ```yaml
  imports:
    fmt: "fmt"                                    # Standard import: import "fmt"
    ctx: "context"                                # Aliased import: import ctx "context"
    _: "unsafe"                                   # Blank import: import _ "unsafe"
  ```

---

## Rule Types

There are several types of rules, each designed for a specific kind of code modification.

### 1. Function Hook Rule

This is the most common rule type. It injects function calls at the beginning (`before`) and/or end (`after`) of a target function or method.

**Use Cases:**

- Wrapping functions with tracing spans.
- Adding logging statements to function entries and exits.
- Recording metrics about function calls.

**Fields:**

- `func` (string, required): The name of the target function to be instrumented.
- `recv` (string, optional): The receiver type for a method. For a standalone function, this field should be omitted. For a pointer receiver, it should be prefixed with `*`, e.g., `*MyStruct`.
- `before` (string, optional): The name of the function to be called at the entry of the target function.
- `after` (string, optional): The name of the function to be called just before the target function returns.
- `path` (string, required): The import path for the package containing the `before` and `after` hook functions.

**Example:**

```yaml
hook_helloworld:
  target: main
  func: Example
  before: MyHookBefore
  after: MyHookAfter
  path: "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/helloworld"
```

This rule will inject `MyHookBefore` at the start of the `Example` function in the `main` package, and `MyHookAfter` at the end. The hook functions are located in the specified `path`.

The tool automatically reads the hook source file and ensures all of its imports are present in the build. No `imports:` field is needed for function hook rules.

### 2. Struct Field Injection Rule

This rule adds one or more new fields to a specified struct type.

**Use Cases:**

- Adding a context field to a struct to enable tracing through its methods.
- Extending existing data structures with new information without modifying the original source code.

**Fields:**

- `struct` (string, required): The name of the target struct.
- `new_field` (list of objects, required): A list of new fields to add to the struct. Each object in the list must contain:
  - `name` (string, required): The name of the new field.
  - `type` (string, required): The Go type of the new field.

**Example:**

```yaml
add_new_field:
  target: main
  struct: MyStruct
  new_field:
    - name: NewField
      type: string
```

This rule adds a new field named `NewField` of type `string` to the `MyStruct` struct in the `main` package.

**Import Handling:**

If your new struct fields use types from external packages, specify those imports:

Example:

```yaml
add_context_field:
  target: main
  struct: MyStruct
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

**Fields:**

- `func` (string, required): The name of the target function.
- `recv` (string, optional): The receiver type for a method.
- `raw` (string, required): The raw Go code to be injected. The code will be inserted at the beginning of the target function.
- `imports` (map[string]string, optional): A map of imports to inject into the target file. Required when the injected code references packages not already imported by the target. Same format as [Common Fields](#common-fields).

**Example:**

```yaml
raw_helloworld:
  target: main
  func: Example
  raw: "go func(){ println(\"RawCode\") }()"
```

This rule injects a new goroutine that prints "RawCode" at the start of the `Example` function in the `main` package.

**Example with imports:**

Raw code frequently references packages that the target file does not already import. Use the `imports:` field to inject those declarations:

```yaml
raw_with_hash:
  target: main
  func: Example
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

### 4. Call Wrapping Rule

This rule wraps function calls at call sites with instrumentation code. Unlike the Function Hook Rule which instruments function definitions, this rule instruments where functions are called.

**Use Cases:**

- Wrapping HTTP client calls with tracing.
- Adding context to database query calls.
- Monitoring third-party library calls without modifying the library.
- Call-site specific instrumentation (different behavior per call location).

**Fields:**

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `function_call` | string | Yes | Qualified function name: `package/path.FunctionName` |
| `replace` | string | No (one of `replace`/`append_args` required) | Wrapper template with `{{ . }}` placeholder for the original call. Must produce a valid Go expression. |
| `append_args` | `[]string` | No (one of `replace`/`append_args` required) | Go expression strings appended as additional arguments to the matched call |
| `variadic_type` | string | No | Element type for the ellipsis IIFE wrapper (e.g. `grpc.DialOption`). Required when any matched call uses `...` spread. |
| `imports` | map[string]string | No | Additional imports needed for injected code (alias: path). Packages must be in the target module's `go.mod`. |

**`replace` and `append_args` are independent and can both be set.** When both are present, `append_args` is applied first (arguments are appended to the call), then `replace` wraps the modified call.

**Replacement Template System:**

The `replace` field uses Go's standard `text/template` package for code generation. This provides:

- **Placeholder Substitution**: `{{ . }}` is replaced with the original function call's AST node
- **Type Safety**: The replacement template is compiled at rule creation time and validated
- **Expression Output**: The replacement template must produce a valid Go expression; the result may be any expression type (not limited to call expressions)

Currently supported template features:

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
  function_call: net/http.Get
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
  function_call: github.com/redis/go-redis/v9.Get
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

This example demonstrates the power of the `text/template` system by using an IIFE (Immediately-Invoked Function Expression) to wrap a call with complex logic:

```yaml
wrap_with_unsafe:
  target: client
  function_call: myapp/utils.Helper
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

**Note:** The `unsafe` package must be imported in the target file for this replacement to work.

---

#### Example 4: Appending gRPC Interceptors (non-ellipsis)

```yaml
add_otel_grpc_interceptors:
  target: myapp
  function_call: google.golang.org/grpc.NewClient
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
  function_call: google.golang.org/grpc.Dial
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

- The `{{ . }}` placeholder in `replace` represents the original function call.
- `replace` must be a valid Go expression that includes the placeholder; the result may be any expression type.
- Replacement code can only reference packages and functions that are already imported or defined in the target file.
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

- `directive` (string, required): The directive name to match, without the leading `//`. Must not contain spaces. For example, `otelc:span` matches the comment `//otelc:span`. Note that a space after `//` (e.g., `// otelc:span`) does **not** match — the directive must immediately follow `//`.
- `template` (string, required): Go statements to prepend to each matching function body. Rendered with [fasttemplate](https://github.com/valyala/fasttemplate) using `{{` / `}}` delimiters. The only supported placeholder is `{{FuncName}}`, which is replaced with the name of the annotated function.
- `imports` (map[string]string, optional): Additional imports needed by the injected code. Same format as [Common Fields](#common-fields).

**Template Placeholders:**

| Placeholder | Replaced with |
| --- | --- |
| `{{FuncName}}` | The name of the annotated function |

**Example:**

```yaml
span_directive:
  target: main
  directive: "otelc:span"
  template: |-
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

**Fields:**

- `file` (string, required): The name of the new file to be added (e.g., `newfile.go`).
- `path` (string, required): The import path of the package where the content of the new file is located. The instrumentation tool will find the file within this package.

**Example:**

```yaml
add_new_file:
  target: main
  file: "new_helpers.go"
  path: "github.com/my-org/my-repo/instrumentation/helpers"
```

This rule would take the file `new_helpers.go` from the `github.com/my-org/my-repo/instrumentation/helpers` package and add it to the `main` package during compilation.

**Import Handling:**

File rules typically don't need the `imports` field because the added file already contains its own import declarations. However, you can use it to add additional imports to the file being added:

Example:

```yaml
add_file_with_extra_imports:
  target: main
  file: "helpers.go"
  path: "github.com/my-org/my-repo/instrumentation/helpers"
  imports:
    log: "log"  # Add extra import to the copied file
```

### 7. Named Declaration Rule

This rule targets a named package-level symbol (variable, constant, function, or type) and either replaces or wraps its initializer. It is the primary mechanism for overriding default values in third-party packages without modifying their source — for example, replacing or wrapping a default HTTP transport with an instrumented one to enable distributed tracing.

**Use Cases:**

- Replacing a package-level `var` with an instrumented implementation (e.g., `http.DefaultTransport`).
- Wrapping an existing package-level `var` initializer with an OTel instrumentation layer.
- Toggling a package-level flag or sentinel value for observability purposes.
- Substituting a registered implementation at compile time.

**Fields:**

- `kind` (string, optional): Constrains the kind of symbol to match. Valid values: `var`, `const`, or omitted/empty to match any kind. (`func` and `type` are recognized but not currently supported — no action can be applied to them.)
- `identifier` (string, required): The name of the top-level symbol to match.
- `replace` (string, optional): A Go expression to assign as the new value of the matched `var` or `const`. Mutually exclusive with `wrap`. Not valid when `kind` is `func` or `type`.
- `wrap` (string, optional): A Go expression template that wraps the existing initializer of the matched `var` or `const`. `{{ . }}` is substituted with the original expression. Mutually exclusive with `replace`. Not valid when `kind` is `func` or `type`.
- `imports` (map[string]string, optional): Additional imports needed by the injected expression. Same format as [Common Fields](#common-fields).

> **Note:** Exactly one of `replace` or `wrap` must be set.

**Example (replace):**

```yaml
assign_default_transport:
  target: net/http
  kind: var
  identifier: DefaultTransport
  replace: |
    &http.Transport{
      MaxIdleConns:    100,
      MaxConnsPerHost: 100,
    }
  imports:
    http: "net/http"
```

This rule replaces `http.DefaultTransport` in the `net/http` package with a custom `*http.Transport` at compile time, enabling all outbound HTTP calls to use the configured transport.

**Example (wrap):**

```yaml
wrap_default_transport:
  target: net/http
  kind: var
  identifier: DefaultTransport
  wrap: "otelhttp.NewTransport({{ . }})"
  imports:
    otelhttp: "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
```

This rule wraps the existing `http.DefaultTransport` value with `otelhttp.NewTransport`, injecting OTel tracing into all outbound HTTP calls without replacing the transport configuration.

**Notes:**

- `replace` must be a valid Go expression (not a statement).
- `wrap` must contain `{{ . }}` as a placeholder for the original expression. Variants `{{.}}`, `{{- . -}}`, etc. are also accepted. The template must produce exactly one expression statement.
- `wrap` returns an error at instrumentation time if the matched declaration has no initializer (e.g., `var X T` without `= ...`).
- If `replace` matches multiple names in a single declaration (e.g., `var a, b = ...`), the replacement expression is cloned and assigned to each name.
- If `wrap` matches multiple initialized values in a single declaration, each initializer is wrapped independently.
- Omitting `kind` matches the first symbol with the given name regardless of kind.
