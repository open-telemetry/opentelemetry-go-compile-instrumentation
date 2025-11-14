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
	instrumenter "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api"
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

var helloWorldInstrumenter = BuildNetHttpClientOtelInstrumenter()

func MyHookBefore(ictx inst.HookContext) {
	// Use instrumenter to create span and metrics
	// When the main is executed, we should instrumenter#start to create span
	ctx := context.Background()
	// We should assign the returned context to ctx variable to make sure the context to be propagated properly
	fmt.Println("[MyHook] start to instrument hello world!")
	ctx = helloWorldInstrumenter.Start(ctx, HelloWorldRequest{})
	// biz logic
	// .........
	// .........
	// .........
	time.Sleep(2 * time.Second)
	// We should use instrumenter#end to end the span and to aggregate the metrics
	helloWorldInstrumenter.End(ctx, instrumenter.Invocation[HelloWorldRequest, HelloWorldResponse]{
		Request:  HelloWorldRequest{},
		Response: HelloWorldResponse{},
	})
	fmt.Println("[MyHook] hello world is instrumented!")
	time.Sleep(2 * time.Second)
}

func MyHookAfter(ictx inst.HookContext) {
	// This is the after hook, we can do some clean up work here if needed
	fmt.Println("[MyHook] after hook executed!")
}

func MyHook1Before(ictx inst.HookContext, recv interface{}) {
	println("Before MyStruct.Example()")
}

func MyHook1After(ictx inst.HookContext) {
	println("After MyStruct.Example()")
}

func BeforeUnderscore(ictx inst.HookContext, _ int, _ float32) {}
