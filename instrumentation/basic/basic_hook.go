// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package basic

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
)

func MyHookBefore(ictx hook.HookContext) {
	// Use direct tracer to create span - simpler pattern
	ctx := context.Background()

	fmt.Println("[MyHook] start to instrument hello world!")

	// Create request with some demo attributes
	req := HelloWorldRequest{
		Path: "/api/hello",
		Params: map[string]string{
			"name": "world",
		},
	}

	// Start instrumentation
	ctx, span := StartInstrumentation(ctx, req)

	// Simulate some work
	time.Sleep(2 * time.Second)

	// End instrumentation
	resp := HelloWorldResponse{
		Status: 200,
	}
	EndInstrumentation(span, resp)

	fmt.Println("[MyHook] hello world is instrumented!")
	time.Sleep(2 * time.Second)
}

func MyHookAfter(ictx hook.HookContext) {
	// This is the after hook, we can do some clean up work here if needed
	fmt.Println("[MyHook] after hook executed!")
}

func MyHook1Before(ictx hook.HookContext, recv interface{}) {
	println("Before MyStruct.Example()")
	fmt.Printf("funcName:%s\n", ictx.GetFuncName())
	fmt.Printf("packageName:%s\n", ictx.GetPackageName())
	fmt.Printf("paramCount:%d\n", ictx.GetParamCount())
	fmt.Printf("returnValCount:%d\n", ictx.GetReturnValCount())
	fmt.Printf("isSkipCall:%t\n", ictx.IsSkipCall())
}

func MyHook2Before(ictx hook.HookContext, recv interface{}) {
	println("Before MyStruct.Example2()")
}

func MyHook1After(ictx hook.HookContext) {
	println("After MyStruct.Example()")
}

func MyHookRecvBefore(ictx hook.HookContext, recv, _ interface{}) {
	println("GenericRecvExample before hook")
}

func MyHookRecvAfter(ictx hook.HookContext, _ interface{}) {
	println("GenericRecvExample after hook")
}

func MyHookGenericBefore(ictx hook.HookContext, _, _ interface{}) {
	println("GenericExample before hook")
	fmt.Printf("[Generic] Function: %s.%s\n", ictx.GetPackageName(), ictx.GetFuncName())
	fmt.Printf("[Generic] Param count: %d\n", ictx.GetParamCount())
	fmt.Printf("[Generic] Skip call: %v\n", ictx.IsSkipCall())
	ictx.SetData("test-data")

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[Generic] SetParam panic (expected): %v\n", r)
		}
	}()
	ictx.SetParam(0, 999)
}

func MyHookGenericAfter(ictx hook.HookContext, _ interface{}) {
	println("GenericExample after hook")
	fmt.Printf("[Generic] Data from Before: %v\n", ictx.GetData())
	fmt.Printf("[Generic] Return value count: %d\n", ictx.GetReturnValCount())

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[Generic] SetReturnVal panic (expected): %v\n", r)
		}
	}()
	ictx.SetReturnVal(0, 999)
}

func BeforeUnderscore(ictx hook.HookContext, _ int, _ float32) {
	println("Underscore")
}

func MyHookEllipsisBefore(ictx hook.HookContext, p1 ...string) {
	println("Ellipsis")
}

// AutoDetectBefore is a hook that imports "github.com/google/uuid", which is
// not imported by the demo/basic app. This verifies that auto-detection adds
// the package to the build importcfg without requiring a manual imports: field.
func AutoDetectBefore(ictx hook.HookContext) {
	fmt.Printf("AutoDetect: %s\n", uuid.Nil.String())
}

func FunctionABefore(ictx hook.HookContext, ctx context.Context) {
	ctx, span := tracer.Start(ctx, "FunctionA")
	ictx.SetParam(0, ctx)
	fmt.Printf(
		"FunctionABefore: TraceID: %s, SpanID: %s\n",
		span.SpanContext().TraceID().String(),
		span.SpanContext().SpanID().String(),
	)
}

func FunctionBBefore(ictx hook.HookContext, ctx context.Context) {
	ctx, span := tracer.Start(ctx, "FunctionB")
	ictx.SetParam(0, ctx)
	fmt.Printf(
		"FunctionBBefore: TraceID: %s, SpanID: %s\n",
		span.SpanContext().TraceID().String(),
		span.SpanContext().SpanID().String(),
	)
}

func UnnamedBefore(ictx hook.HookContext, recv interface{}, arg1 int, arg2 float32) {
	fmt.Printf("UnnamedBefore %v %v\n", arg1, arg2)
}
