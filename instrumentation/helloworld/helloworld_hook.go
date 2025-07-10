// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package helloworld

import (
	"context"
	"fmt"
	"time"
	_ "unsafe"

	instrumenter "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
)

func init() {
	setupOpenTelemetry()
}

func setupOpenTelemetry() {
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

//go:linkname MyHook main.Hook
func MyHook() {
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
	time.Sleep(5 * time.Second)
	// We should use instrumenter#end to end the span and to aggregate the metrics
	helloWorldInstrumenter.End(ctx, instrumenter.Invocation[HelloWorldRequest, HelloWorldResponse]{
		Request:  HelloWorldRequest{},
		Response: HelloWorldResponse{},
	})
	fmt.Println("[MyHook] hello world is instrumented!")
	time.Sleep(2 * time.Second)
}
