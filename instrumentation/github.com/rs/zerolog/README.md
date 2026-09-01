# `zerolog` Instrumentation

This document explains how the `github.com/rs/zerolog` instrumentation provided by this repository works.

## Overview

`instrumentation/github.com/rs/zerolog` hooks `zerolog.New` after it creates a logger. It adds a zerolog hook to that logger, which appends `trace_id` and `span_id` fields when an active span is available. Zerolog copies hooks to derived loggers, so this covers local, derived, and package-global loggers created through `New`.

The instrumentation is enabled by default and can be configured with the `logs/zerolog` key in `OTEL_GO_ENABLED_INSTRUMENTATIONS` or `OTEL_GO_DISABLED_INSTRUMENTATIONS`.

## How trace and span IDs are attached

The hook calls `runtime.GetTraceAndSpanID()`, which returns the current goroutine's active trace and span IDs from goroutine-local storage (GLS), or two empty strings if none is available. If the trace ID is empty, the event is left unchanged. The span ID is added only when it is non-empty.

**`go.opentelemetry.io/otel/sdk/trace` must already be part of the application's build dependency graph (as seen by `go list -deps` / `go build -a -x -n`) for this instrumentation to inject `trace_id`/`span_id`.**

Instrumentation imports declared in an `otel.instrumentation.go` file are tracked in `go.mod` but are intentionally **not** added to the build's dependency graph — that file is marked with the `//go:build tools` build tag specifically so instrumentation imports do not become build dependencies themselves. So relying on `logrus`'s own instrumentation module to pull in `sdk/trace` does not work.

In practice, all that's needed is a plain blank import of the package somewhere in your own application source, outside of any `//go:build tools`-tagged file:

```go
import _ "go.opentelemetry.io/otel/sdk/trace"
```

That's enough to put it in the build graph — you don't need to actually construct or use a `TracerProvider` yourself. Without it, spans are still created and exported correctly, but `logrus` output will not carry `trace_id`/`span_id`.

If `go.opentelemetry.io/otel/sdk/trace` instrumentation was not applied (for example because the package was not present in the application's build graph), or if there is no active span in GLS, entries are left unchanged — see [`instrumentation/go.opentelemetry.io/otel`'s README](../../../go.opentelemetry.io/otel/README.md) for how GLS is populated.
