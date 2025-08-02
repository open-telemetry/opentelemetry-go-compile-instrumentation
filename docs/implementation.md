# Introduction

This document outlines a method for injecting runtime hooks into target functions
in Go programs, enabling dynamic monitoring and modification of function behavior.
The approach leverages trampoline code injection and function pointer redirection
to seamlessly integrate monitoring logic without requiring significant changes to
the user's codebase.

The goal is to provide a flexible and non-intrusive mechanism for instrumenting
Go applications, particularly for use cases such as observability (e.g., OpenTelemetry),
debugging, security or performance profiling.

# Core Principles

## 1. Trampoline Code Injection

Trampoline function calls are injected into the Target (lib-side) function, which
ultimately jumps to the actual hook code via the function pointer Hook. The two-level
indirection (trampoline call to hook) is a deliberate design choice that offers
several key benefits:

- Exception Handling: The trampoline catches panics and isolates exception handling,
  preventing them from affecting the target function or hook code.
- Context Construction: The trampoline initializes and manages the necessary
  context before invoking the hook code.
- Decoupling: The trampoline decouples the hook code from the target function,
  enabling flexibility and maintainability.
- Debugging: The trampoline provides a centralized point for debugging and observability.
- Dynamic Instrumentation: The trampoline allows turn-on/off instrumentation
  at runtime, enabling dynamic control over monitoring behavior.

The code snippet below illustrates the two-level indirection, where the Target
function is the target function to be instrumented, and the Hook function is the
monitoring code that will be executed.

```go
func Target() {
    Trampoline()
    ....
}

func Trampoline() {
    Hook()
}

//go:linkname Hook github.com/open-telemetry/opentelemetry-go-compile-instrumentation/sdk/hook.MyHook
var Hook func()
```

## 2. Linkage via golinkname

The `Hook` function is linked to the monitoring code using the `//go:linkname`
directive. This allows us to dynamically associate the hook with the target
function at compile time.

# Implementation Details

Go compile-time instrumentation is a two-phase process: the first phase involves
setup dependencies, and the second phase focuses on building the project using
a custom toolchain.

## Phase 1: Setup Dependencies

### 1.1 Dependency Analysis

The first step is to analyze the project's dependencies by collecting the list
of modules involved in the build. This is done using the `go build -n` command,
which prints the build plan without executing it.

```command
go build -n > build_plan.txt
```

From the build_plan.txt file, the tool extracts all third-party module paths
(e.g., `github.com/go-redis/redis`, `github.com/gin-gonic/gin`).

## 1.2 Add Dependencies

The hook configuration is specified by [ux-design.md](ux-design.md),
which includes the `ImportPath` field where the target function resides. The tool
matches this `ImportPath` against the pre-collected third-party dependencies and
generates a file (e.g., otel_import.go) to import the SDK and corresponding hook
packages for matched dependency

```go
// otel_importer.go
package main

// Import the SDK for shared utilities
import _ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/sdk"

// Import hooks for specific third-party libraries
import _ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/sdk/hook/redis"
import _ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/sdk/hook/gin"
```

After adding the dependency, `go mod tidy` is run to update the `go.mod` file.
This step ensures that the dependency is properly recorded and downloaded into
the module cache.
By completing this phase, the project is now ready for the next step, where the
instrumentation logic will be applied during the build process.

## Phase 2: Building with Instrumentation

The build process can be integrated with custom toolchains in the following ways:

- Command Prefix: `otel go build` (simple but requires manual prefixing)
- Environment Variable: `GOFLAGS=-toolexec=otel toolexec` (global effect; no per-command setup)
- Direct flag: `go build -toolexec=otel toolexec` (on-demand use; ideal for scripts/CI)

All of these leverage the `-toolexec` flag, which allows users to specify a
custom tool (e.g., otel) that intercepts compilation commands. The tool identifies
the target function from the compilation commands and injects trampoline code
into the AST of these functions. Since the hook dependency was already imported
in Phase 1, the tool can link the target function to the hook code via `//go:linkname`
without requiring any additional modifications.

# Interface Design

## 1. Context

The Context interface is designed to provide a structured way to access and
manipulate the parameters, return values, and other relevant data of the target
function. This allows the hook code to interact with the target function's
execution context seamlessly.

Example:

```go
func MyHookBefore(ctx Context) {
	ctx.GetFuncName()
	ctx.GetParam(1)
	ctx.SetParam(1, "new value")
	ctx.GetReturnValue(1)
	ctx.SetReturnValue(1, "new value")
	ctx.SetData("msg", "hello world")
}
func MyHookAfter(ctx Context) {
	msg := ctx.GetData("msg")
}
```

## 2. Full Context API

The full context API is listed below.

```go
type Context interface {
	// Set the data field, can be used to pass information between OnEnter & OnExit
	SetData(interface{})
	// Get the data field, can be used to pass information between OnEnter & OnExit
	GetData() interface{}
	// Get the map data field by key
	GetKeyData(key string) interface{}
	// Set the map data field by key
	SetKeyData(key string, val interface{})
	// Has the map data field by key
	HasKeyData(key string) bool
	// Get the original function parameter at index idx
	GetParam(idx int) interface{}
	// Change the original function parameter at index idx
	SetParam(idx int, val interface{})
	// Number of parameters in the original function
	NumParams() int
	// Get the original function return value at index idx
	GetReturnVal(idx int) interface{}
	// Change the original function return value at index idx
	SetReturnVal(idx int, val interface{})
	// Number of return values in the original function
	NumReturnVals() int
	// Get the original function name
	GetFuncName() string
	// Get the package name of the original function
	GetPackageName() string
}
```

Key Features:

- Parameter Access and Modification: Allows for the inspection and modification
  of function arguments.
- Return Value Manipulation: Enables the modification of return values before
  they are returned to the caller.
- State Management: Facilitates communication between the OnEnter and OnExit
  phases of the hook.
