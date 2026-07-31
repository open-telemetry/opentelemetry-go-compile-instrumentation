# OpenTelemetry SDK Instrumentation

This document explains how the OpenTelemetry SDK instrumentation provided by this repository works.

## Overview

The instrumentation is split across independent modules that are applied based on the application's build graph.

The provided modules are:

* `instrumentation/go.opentelemetry.io/otel/init`
* `instrumentation/go.opentelemetry.io/otel/sdk/trace`
* `instrumentation/go.opentelemetry.io/otel/trace`

## Main Components

### 1) SDK initialization

`instrumentation/go.opentelemetry.io/otel/init` automatically initializes the OpenTelemetry SDK during program startup.

It also initializes OpenTelemetry runtime metrics collection.

### 2) SDK trace instrumentation

`instrumentation/go.opentelemetry.io/otel/sdk/trace` is applied when `go.opentelemetry.io/otel/sdk/trace` is present in the application's build graph.

This instrumentation maintains the active span chain in goroutine-local storage (GLS):

* on recording span creation, the span is added to GLS
* on non-recording span creation, the span is added to GLS
* on span end, the span is removed from GLS

It uses the low-level GLS accessors provided by the instrumented runtime:

* `GetTraceContextFromGLS()`
* `SetTraceContextToGLS(interface{})`

If this instrumentation is not applied, no span state is stored in GLS.

### 3) Trace API instrumentation

`instrumentation/go.opentelemetry.io/otel/trace` is applied when `go.opentelemetry.io/otel/trace` is present in the application's build graph.

This instrumentation hooks `trace.SpanFromContext` to provide a GLS fallback.

Whenever `trace.SpanFromContext` returns, the hook checks whether the returned span contains a valid `SpanContext`. If it does, the return value is left unchanged. Otherwise, the hook attempts to retrieve the current span from GLS through the instrumented runtime and rewrites the return value if a span is found. If no span exists in GLS, or if the `go.opentelemetry.io/otel/sdk/trace` instrumentation was not applied (for example because the package was not present in the application's build graph), the original return value is left unchanged.

## Runtime Support

The instrumented runtime (`instrumentation/runtime/runtime_gls.go`) provides the low-level GLS accessors used by the SDK trace instrumentation:

* `GetTraceContextFromGLS()`
* `SetTraceContextToGLS(interface{})`
* `GetBaggageContainerFromGLS()`
* `SetBaggageContainerToGLS(interface{})`

It also defines `OtelContextCloner` for goroutine propagation logic.

## Operational Notes

* GLS state is scoped to a goroutine. Correct context propagation across goroutines still depends on runtime propagation hooks.
* Each instrumentation module is applied independently based on the application's build graph.
* The GLS fallback only applies when `go.opentelemetry.io/otel/trace` instrumentation is present.
* This mechanism is intended for compile-time instrumentation internals; it is not a public API contract.
