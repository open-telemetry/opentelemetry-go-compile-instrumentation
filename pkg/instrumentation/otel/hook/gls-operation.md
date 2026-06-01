# GLS Operation for OTel SDK Instrumentation

This document explains how goroutine-local storage (GLS) is used by the OTel SDK instrumentation in this repository.

## Background

The OTel SDK normally propagates span context via `context.Context`. Some code paths still call APIs such as `trace.SpanFromContext(context.Background())`, where no span exists in the provided context.

To improve compatibility, this project stores the active span chain in goroutine-local storage and bridges selected OTel SDK APIs to that state during compile-time instrumentation.

## High-Level Flow

The GLS flow is implemented through three parts:

1. Runtime GLS fields and helpers in the instrumented runtime package.
2. Injected OTel SDK trace helper file (`otel_trace_context.go`).
3. Hook rules that add/remove/read spans at key OTel SDK call sites.

At runtime:

- On span creation (`newRecordingSpan`, `newNonRecordingSpan`), the new span is added to GLS.
- On span end (`recordingSpan.End`, `nonRecordingSpan.End`), the span is removed from GLS.
- On `trace.SpanFromContext`, if the original return span is invalid, the hook tries GLS as a fallback.

## Main Components

### 1) Runtime GLS accessors

`pkg/instrumentation/runtime/runtime_gls.go` provides low-level accessors:

- `GetTraceContextFromGLS()`
- `SetTraceContextToGLS(interface{})`
- `GetBaggageContainerFromGLS()`
- `SetBaggageContainerToGLS(interface{})`

It also defines `OtelContextCloner` for goroutine propagation logic.

### 2) Injected trace context holder

`pkg/instrumentation/otel/sdk/trace/otel_trace_context.go` defines an internal linked-list based trace context container in GLS:

- add span to current goroutine context
- delete span when ended
- fetch current span for fallback lookup

The max chain size is configurable:

- env var: `OTEL_GLS_MAX_SPANS`
- default: `1000`
- invalid or non-positive values are ignored (default remains in effect)

### 3) Hook integration points

Configured in `pkg/instrumentation/otel/hook/hooks.yaml` and implemented in `pkg/instrumentation/otel/hook/`:

- `tracer_setup.go`: add span to GLS after span creation
- `span_setup.go`: remove span from GLS before span end
- `span_context.go`: fallback to GLS in `trace.SpanFromContext`

## Why GLS is Needed

GLS fallback is useful for compatibility with existing code that:

- does not pass context through all call boundaries
- uses `context.Background()` at read points
- expects current span lookup to still work in instrumented binaries

This is especially helpful for auto-instrumentation scenarios where user code is unchanged.

## Operational Notes

- GLS state is scoped to a goroutine. Correct context propagation across goroutines still depends on runtime propagation hooks.
- The fallback behavior only applies where configured by instrumentation rules.
- This mechanism is intended for compile-time instrumentation internals; it is not a public API contract.

## Troubleshooting

- If GLS fallback seems ineffective, verify OTel hook rules were matched and applied.
- Confirm your build uses `otelc` (`-toolexec`) and instrumentation is enabled.
- Check whether spans are being ended earlier than expected, which removes them from GLS.
