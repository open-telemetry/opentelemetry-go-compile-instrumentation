# `log/slog` Instrumentation

This document explains how the `log/slog` instrumentation provided by this repository works.

## Overview

`instrumentation/log/slog` hooks `slog.NewRecord` to append `trace_id`/`span_id` attributes to the created record when an active span is available.

## How trace/span ids are attached

The hook calls `runtime.GetTraceAndSpanID()` (see `pkg/runtime/trace_context.go`), which returns the current goroutine's active trace/span id from goroutine-local storage (GLS), or two empty strings if none is available. If the returned trace id is empty, no attributes are added to the record.

`GetTraceAndSpanID()` only returns real data once `go.opentelemetry.io/otel/sdk/trace`'s own instrumentation (`instrumentation/go.opentelemetry.io/otel/sdk/trace`) has registered its implementation via `runtime.RegisterTraceAndSpanIDFunc`. That registration only happens when `go.opentelemetry.io/otel/sdk/trace` is applied — which, in turn, requires the package to be part of the application's build graph.

## Limitation: `go.opentelemetry.io/otel/sdk/trace` must be in the build graph

**`go.opentelemetry.io/otel/sdk/trace` must already be part of the application's build dependency graph (as seen by `go list -deps` / `go build -a -x -n`) for this instrumentation to inject `trace_id`/`span_id`.**

Instrumentation imports declared in an `otel.instrumentation.go` file are tracked in `go.mod` but are intentionally **not** added to the build's dependency graph — that file is marked with the `//go:build tools` build tag specifically so instrumentation imports do not become build dependencies themselves. So relying on `log/slog`'s own instrumentation module to pull in `sdk/trace` does not work.

In practice, all that's needed is a plain blank import of the package somewhere in your own application source, outside of any `//go:build tools`-tagged file:

```go
import _ "go.opentelemetry.io/otel/sdk/trace"
```

That's enough to put it in the build graph — you don't need to actually construct or use a `TracerProvider` yourself. Without it, spans are still created and exported correctly, but `slog` output will not carry `trace_id`/`span_id`.

If `go.opentelemetry.io/otel/sdk/trace` instrumentation was not applied (for example because the package was not present in the application's build graph), or if there is no active span in GLS, the record is left unchanged — see [`instrumentation/go.opentelemetry.io/otel`'s README](../../go.opentelemetry.io/otel/README.md) for how GLS is populated.
