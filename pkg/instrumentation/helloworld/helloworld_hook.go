// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package helloworld

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

func init() {
	setupOpenTelemetry()
}

func setupOpenTelemetry() {
	fmt.Println("=setupOpenTelemetry=")
	// Print all the signal to stdout
	spanExporter, _ := stdouttrace.New()
	stdoutTraceProvider := trace.NewTracerProvider(trace.WithSpanProcessor(trace.NewSimpleSpanProcessor(spanExporter)))
	otel.SetTracerProvider(stdoutTraceProvider)
	metricExporter, _ := stdoutmetric.New()
	stdoutMeterProvider := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExporter, metric.WithInterval(1*time.Second))),
	)
	otel.SetMeterProvider(stdoutMeterProvider)
}

func MyHookBefore(ictx inst.HookContext) {
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

func MyHookAfter(ictx inst.HookContext) {
	// This is the after hook, we can do some clean up work here if needed
	fmt.Println("[MyHook] after hook executed!")
}

func MyHook1Before(ictx inst.HookContext, recv interface{}) {
	println("Before MyStruct.Example()")
	fmt.Printf("funcName:%s\n", ictx.GetFuncName())
	fmt.Printf("packageName:%s\n", ictx.GetPackageName())
	fmt.Printf("paramCount:%d\n", ictx.GetParamCount())
	fmt.Printf("returnValCount:%d\n", ictx.GetReturnValCount())
	fmt.Printf("isSkipCall:%t\n", ictx.IsSkipCall())
}

func MyHook1After(ictx inst.HookContext) {
	println("After MyStruct.Example()")
}

func MyHookRecvBefore(ictx inst.HookContext, recv, _ interface{}) {
	println("GenericRecvExample before hook")
}

func MyHookRecvAfter(ictx inst.HookContext, _ interface{}) {
	println("GenericRecvExample after hook")
}

func MyHookGenericBefore(ictx inst.HookContext, _, _ interface{}) {
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

func MyHookGenericAfter(ictx inst.HookContext, _ interface{}) {
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

func BeforeUnderscore(ictx inst.HookContext, _ int, _ float32) {
	println("Underscore")
}

func MyHookEllipsisBefore(ictx inst.HookContext, p1 []string) {
	println("Ellipsis")
}

func FunctionABefore(ictx inst.HookContext, ctx context.Context) {
	ctx, span := tracer.Start(ctx, "FunctionA")
	ictx.SetParam(0, ctx)
	fmt.Printf("FunctionABefore: TraceID: %s, SpanID: %s\n", span.SpanContext().TraceID().String(), span.SpanContext().SpanID().String())
}

func FunctionBBefore(ictx inst.HookContext, ctx context.Context) {
	ctx, span := tracer.Start(ctx, "FunctionB")
	ictx.SetParam(0, ctx)
	fmt.Printf("FunctionBBefore: TraceID: %s, SpanID: %s\n", span.SpanContext().TraceID().String(), span.SpanContext().SpanID().String())
}
