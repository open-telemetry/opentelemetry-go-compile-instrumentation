# Instrumentation Rules Documentation

This document explains the different types of instrumentation rules used by the Go compile-time instrumentation tool. These rules, defined in YAML files, allow for the injection of code into target Go packages.

## Common Fields

All rules share a set of common fields that define the target of the instrumentation.

- `target` (string, required): The import path of the Go package to be instrumented. For example, `golang.org/x/time/rate` or `main` for the main package.
- `version` (string, optional): Specifies a version range for the target package. The rule will only be applied if the package's version falls within this range. The format is `start_inclusive,end_exclusive`. For example, `v0.11.0,v0.12.0` means the rule applies to versions greater than or equal to `v0.11.0` and less than `v0.12.0`. If omitted, the rule applies to all versions.

---

## Rule Types

There are four types of rules, each designed for a specific kind of code modification.

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

**Example:**

```yaml
raw_helloworld:
  target: main
  func: Example
  raw: "go func(){ println(\"RawCode\") }()"
```

This rule injects a new goroutine that prints "RawCode" at the start of the `Example` function in the `main` package.

### 4. File Addition Rule

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
