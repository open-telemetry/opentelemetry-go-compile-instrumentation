# Introduction
This proposal outlines a method for injecting runtime hooks into target functions in Go programs, enabling dynamic monitoring and modification of function behavior. The approach leverages trampoline code injection and function pointer redirection to seamlessly integrate monitoring logic without requiring significant changes to the user's codebase.

The goal is to provide a flexible and non-intrusive mechanism for instrumenting Go applications, particularly for use cases such as observability (e.g., OpenTelemetry), debugging, or performance profiling.

# Core Principles
## 1. Trampoline Code Injection
We inject trampoline function call into the Target (lib-side) function, which ultimately jumps to the actual hook code via the function pointer Hook.

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
The two-level indirection (trampoline call to hook) is a deliberate design choice that offers several key benefits:

- Exception Handling: The trampoline catches panics and isolates exception handling, preventing them from affecting the target function or hook code.
- Context Construction: The trampoline initializes and manages the necessary context before invoking the hook code.
- Decoupling: The trampoline decouples the hook code from the target function, enabling flexibility and maintainability.
- Debugging: The trampoline provides a centralized point for debugging and observability.


## 2. Linkage via golinkname
The `Hook` function is linked to the monitoring code using the `//go:linkname` directive. This allows us to dynamically associate the hook with the target function at compile time.

# Implementation Details
Go compile-time instrumentation is a two-phase process: the first phase involves updating dependencies, and the second phase focuses on building the project using a custom toolchain.

## Phase 1: Adding Dependencies
To prepare the project for instrumentation, we first integrate the necessary dependencies into the user's codebase. This step ensures that the required hook code is available during the build process.

This can be done by creating or modifying files. For example:

```go
// otel_importer.go
package main

import _ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/sdk"
```

This import statement ensures that the SDK, which contains the hook code, is included in the project. The `_` prefix indicates that the package is imported only for its side effects (e.g., registration of hooks).

After adding the dependency, we run `go mod tidy` to update the `go.mod` file. This step ensures that the dependency is properly recorded and downloaded into the module cache.
By completing this phase, the project is now ready for the next step, where the instrumentation logic will be applied during the build process.

## Phase 2: Building with Instrumentation
The build process is invoked with a custom toolchain by specifying the `-toolexec=otel` flag

```bash
go build -toolexec=otel
```

The `-toolexec=otel` flag specifies a custom tool (e.g., otel) that intercepts compilation commands. We find the target function from compilation commands and inject trampoline code into the AST of these functions. Since the hook dependency was already imported in Phase 1, we can link the target function to the hook code via golinkname without any other modifications.


# Interface Design
## 1. Context
The Context interface is designed to provide a structured way to access and manipulate the parameters, return values, and other relevant data of the target function. This allows the hook code to interact with the target function's execution context seamlessly.

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
	// Skip the original function call
	SetSkipCall(bool)
	// Check if the original function call should be skipped
	IsSkipCall() bool
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
	// Get the original function return value at index idx
	GetReturnVal(idx int) interface{}
	// Change the original function return value at index idx
	SetReturnVal(idx int, val interface{})
	// Get the original function name
	GetFuncName() string
	// Get the package name of the original function
	GetPackageName() string
}
```

Key Features:
- Parameter Access and Modification: Allows inspection and alteration of function arguments.
- Return Value Manipulation: Enables modification of return values before they are passed back to the caller.
- Flow Control: Provides the ability to skip the original function call entirely (SetSkipCall).
- State Management: Facilitates communication between the OnEnter and OnExit phases of the hook.