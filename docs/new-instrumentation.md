# Adding a New Library Instrumentation

This guide walks through the full process of adding compile-time instrumentation for a new Go library. By the end you will have a working hook package, YAML rule(s), unit tests, a test application, and an integration test.

Before reading this guide, familiarise yourself with the big picture in `docs/rules.md` (YAML rule reference) and `docs/testing.md` (test categories and when to use each).

## Overview

`otelc` rewrites target Go programs at compile time. It does not require the application to import any instrumentation package. Instead, for each library you want to instrument, you:

1. Write a **hook package** — a regular Go package containing `BeforeXxx` and `AfterXxx` functions.
2. Write a **YAML rule file** co-located with the hook package. The rule tells `otelc` which function to intercept and which hook functions to call.
3. Ship everything as an independent Go module inside `pkg/instrumentation/<name>/`.

When `otelc` builds the target program, it discovers all YAML rules bundled into the tool (via `make package`), injects the hook calls into the target's AST, and compiles the result. The hook package is pulled in as a dependency automatically.

## Quick-reference checklist

Use this as a todo list while working through the steps.

- [ ] Create `pkg/instrumentation/<name>/` directory layout
- [ ] Write `go.mod` with `replace` directives for `pkg` and `shared`
- [ ] Run `make crosslink && make go-mod-tidy`
- [ ] Write `<name>.yaml` with function hook rule(s)
- [ ] Write `<name>_hook.go` with enabler, `initInstrumentation`, `BeforeXxx`, `AfterXxx`
- [ ] Add semconv helpers in `semconv/` sub-package (if needed)
- [ ] Write `<name>_hook_test.go` unit tests
- [ ] Run unit tests: `go test -C pkg/instrumentation/<name> ./...`
- [ ] Create `test/apps/<name>/main.go` and `test/apps/<name>/go.mod`
- [ ] Run `make tidy/test-apps`
- [ ] Write `test/integration/<name>_test.go` with `//go:build integration`
- [ ] Add `RequireXxxSemconv` to `test/testutil/semconv.go` if your semconv is not already there
- [ ] Write E2E test in `test/e2e/` (only for multi-process scenarios)
- [ ] Run `make format/license` to add Apache 2.0 headers
- [ ] Run `make all` (build + format + lint + test)
- [ ] Open a PR following the guidelines in `CONTRIBUTING.md`

---

## Step 1: Create the instrumentation package

### Directory layout

For a single-sided instrumentation (for example, a standalone client library):

```
pkg/instrumentation/<name>/
├── <name>_hook.go
├── <name>.yaml
├── go.mod
├── go.sum
└── semconv/          # optional
    ├── <name>.go
    └── <name>_test.go
```

For libraries that have both a client and a server side (for example, `grpc` or `nethttp`), split the hook code into sub-packages:

```
pkg/instrumentation/<name>/
├── client/
│   ├── client_hook.go
│   ├── client.yaml
│   ├── go.mod
│   └── go.sum
├── server/
│   ├── server_hook.go
│   ├── server.yaml
│   ├── go.mod
│   └── go.sum
└── semconv/
    ├── client.go
    └── server.go
```

See `pkg/instrumentation/nethttp/` for a complete client/server-split example, and `pkg/instrumentation/databasesql/` for a single-module example.

### `go.mod` template

Every hook module must declare `replace` directives for the two in-repo dependencies it always needs:

```
module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/<name>

go 1.25.0

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg => ../../..

replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared => ../../shared

require (
    github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg v0.0.0
    github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared v0.0.0
    go.opentelemetry.io/otel v1.40.0
    go.opentelemetry.io/otel/sdk v1.40.0
    go.opentelemetry.io/otel/trace v1.40.0
    <your-target-library> <version>
)
```

For a client/server split the `replace` paths must be adjusted relative to the sub-package directory. For example, `pkg/instrumentation/nethttp/client/go.mod` uses:

```
replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg => ../../..
replace github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared => ../../shared
```

After creating the file, run:

```bash
make crosslink      # updates all intra-repository replace directives
make go-mod-tidy    # runs go mod tidy in all modules
```

---

## Step 2: Write the YAML rule

Create `<name>.yaml` (or `client.yaml` / `server.yaml`) next to your hook file. The most common rule type is a **function hook**. See `docs/rules.md` for the full reference including struct field injection and other rule types.

### Minimal function hook example

```yaml
hook_<name>:
  target: "import/path/of/the/instrumented/package"
  func: FunctionName
  before: BeforeHookFunc
  after: AfterHookFunc
  path: "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/<name>"
```

### Method hook (with receiver)

```yaml
hook_<name>:
  target: "import/path/of/the/instrumented/package"
  func: MethodName
  recv: "*ReceiverType"
  before: BeforeHookFunc
  after: AfterHookFunc
  path: "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/<name>"
```

### Real example — `nethttp/client/client.yaml`

```yaml
client_hook:
  target: net/http
  func: RoundTrip
  recv: "*Transport"
  before: BeforeRoundTrip
  after: AfterRoundTrip
  path: "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp/client"
```

### Real example — `grpc/server/server.yaml`

```yaml
server_hook:
  target: google.golang.org/grpc
  func: NewServer
  before: BeforeNewServer
  after: AfterNewServer
  path: "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/grpc/server"
```

### The `version` field

Use `version` when a rule only applies to a specific version range of the target library. The format is `start_inclusive,end_exclusive`:

```yaml
hook_foo:
  target: example.com/mylib
  func: Do
  before: BeforeDo
  after: AfterDo
  path: "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/foo"
  version: "v2.0.0,v3.0.0"
```

Omit `version` when the rule applies to all versions of the target package.

### Struct field injection

When the hook needs to store extra state on a target struct (for example, recording the database endpoint on `sql.DB`), use an `InstStructRule`:

```yaml
add_new_field_db:
  target: database/sql
  struct: DB
  new_field:
    - name: Endpoint
      type: string
    - name: DriverName
      type: string
```

See `pkg/instrumentation/databasesql/db.yaml` for a working example. Note the important limitation described in the unit test section below.

---

## Step 3: Write the hook functions

Create `<name>_hook.go` inside your package. The standard structure used by every instrumentation in this repository is:

### Package constants

```go
const (
    instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/<name>"
    instrumentationKey  = "NAME"   // uppercase; used for enable/disable env var lookup
)
```

The `instrumentationKey` must be a single word in uppercase (e.g. `"NETHTTP"`, `"GRPC"`, `"DATABASE"`). It is matched case-insensitively against the value of the `OTEL_GO_ENABLED_INSTRUMENTATIONS` and `OTEL_GO_DISABLED_INSTRUMENTATIONS` environment variables. Users disable your instrumentation by setting:

```
OTEL_GO_DISABLED_INSTRUMENTATIONS=name
```

### Package-level variables

```go
var (
    logger   = shared.Logger()
    tracer   trace.Tracer
    initOnce sync.Once
)
```

### Enabler pattern

```go
type myEnabler struct{}

func (e myEnabler) Enable() bool {
    return shared.Instrumented(instrumentationKey)
}

var clientEnabler = myEnabler{}
```

### Lazy initialisation

All OTel SDK setup goes inside `initInstrumentation`, which is called once via `sync.Once` from the first Before hook that is actually enabled:

```go
func moduleVersion() string {
    bi, ok := debug.ReadBuildInfo()
    if !ok {
        return "dev"
    }
    if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
        return bi.Main.Version
    }
    return "dev"
}

func initInstrumentation() {
    initOnce.Do(func() {
        version := moduleVersion()
        if err := shared.SetupOTelSDK(
            "go.opentelemetry.io/compile-instrumentation/<name>",
            version,
        ); err != nil {
            logger.Error("failed to setup OTel SDK", "error", err)
        }
        tracer = otel.GetTracerProvider().Tracer(
            instrumentationName,
            trace.WithInstrumentationVersion(version),
        )
        if err := shared.StartRuntimeMetrics(); err != nil {
            logger.Error("failed to start runtime metrics", "error", err)
        }
        logger.Info("<Name> instrumentation initialized")
    })
}
```

### Before hook

The Before hook receives `ictx inst.HookContext` as its first argument, followed by the same parameters as the instrumented function (excluding the receiver, which comes before `ictx` in some hook signatures — always verify against the actual function signature you are hooking).

```go
func BeforeFoo(ictx inst.HookContext, arg1 Type1, arg2 Type2) {
    if !clientEnabler.Enable() {
        return
    }
    initInstrumentation()

    ctx := // extract context from arg1/arg2, or use context.Background()

    attrs := // build semconv attributes from args

    ctx, span := tracer.Start(ctx, "span-name",
        trace.WithSpanKind(trace.SpanKindClient), // or Server
        trace.WithAttributes(attrs...),
    )

    // For client-side hooks, inject trace context into outgoing carrier:
    propagator.Inject(ctx, propagation.HeaderCarrier(req.Header))
    // Update the parameter that carries the context:
    ictx.SetParam(requestParamIndex, req.WithContext(ctx))

    // Pass the span to AfterFoo:
    ictx.SetData(map[string]interface{}{
        "ctx":   ctx,
        "span":  span,
        "start": time.Now(),
    })
}
```

Key points:
- Always check `clientEnabler.Enable()` first and return immediately if disabled.
- Call `initInstrumentation()` only after the enable check — it must not run when instrumentation is off.
- Use `ictx.SetParam(index, value)` to replace a function parameter (e.g., to attach a new context to a request object). The `index` is 0-based and counts from the first parameter of the instrumented function (not `ictx`).
- Use `ictx.SetData(map[string]interface{}{...})` to store arbitrary data for the After hook.

### After hook

The After hook receives `ictx inst.HookContext` followed by the return values of the instrumented function.

```go
func AfterFoo(ictx inst.HookContext, result ResultType, err error) {
    if !clientEnabler.Enable() {
        return
    }

    span, ok := ictx.GetKeyData("span").(trace.Span)
    if !ok || span == nil {
        return
    }
    defer span.End()

    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
    }

    if result != nil {
        attrs := // build response attributes
        span.SetAttributes(attrs...)
    }
}
```

Key points:
- Always guard with `clientEnabler.Enable()` first.
- Retrieve the span via `ictx.GetKeyData("span").(trace.Span)`. If the Before hook was skipped (e.g., because it was disabled or filtered), `GetKeyData` returns `nil`; the type assertion then correctly prevents a panic.
- Call `span.End()` via `defer` so it runs even if the After hook returns early.

### Context propagation

For **client-side** hooks, inject the outgoing trace context into the request carrier before handing it to the library:

```go
propagator.Inject(ctx, propagation.HeaderCarrier(req.Header))
newReq := req.WithContext(ctx)
ictx.SetParam(requestParamIndex, newReq)
```

For **server-side** hooks, extract the incoming trace context from the carrier:

```go
ctx = propagator.Extract(ctx, propagation.HeaderCarrier(req.Header))
ctx, span := tracer.Start(
    trace.ContextWithRemoteSpanContext(ctx, trace.SpanContextFromContext(ctx)),
    spanName,
    trace.WithSpanKind(trace.SpanKindServer),
)
```

See `pkg/instrumentation/nethttp/client/client_hook.go` for the client pattern and `pkg/instrumentation/nethttp/server/server_hook.go` for the server pattern.

### Complete minimal example

```go
// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package mylib

import (
    "runtime/debug"
    "sync"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/codes"
    "go.opentelemetry.io/otel/trace"

    "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
    "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
)

const (
    instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/mylib"
    instrumentationKey  = "MYLIB"
)

var (
    logger   = shared.Logger()
    tracer   trace.Tracer
    initOnce sync.Once
)

type mylibEnabler struct{}

func (e mylibEnabler) Enable() bool { return shared.Instrumented(instrumentationKey) }

var enabler = mylibEnabler{}

func moduleVersion() string {
    bi, ok := debug.ReadBuildInfo()
    if !ok {
        return "dev"
    }
    if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
        return bi.Main.Version
    }
    return "dev"
}

func initInstrumentation() {
    initOnce.Do(func() {
        version := moduleVersion()
        if err := shared.SetupOTelSDK("go.opentelemetry.io/compile-instrumentation/mylib", version); err != nil {
            logger.Error("failed to setup OTel SDK", "error", err)
        }
        tracer = otel.GetTracerProvider().Tracer(instrumentationName, trace.WithInstrumentationVersion(version))
        if err := shared.StartRuntimeMetrics(); err != nil {
            logger.Error("failed to start runtime metrics", "error", err)
        }
        logger.Info("mylib instrumentation initialized")
    })
}

func BeforeDo(ictx inst.HookContext, client *mylib.Client, req *mylib.Request) {
    if !enabler.Enable() {
        return
    }
    initInstrumentation()

    ctx, span := tracer.Start(req.Context(), req.Method,
        trace.WithSpanKind(trace.SpanKindClient),
    )
    ictx.SetData(map[string]interface{}{
        "span": span,
        "ctx":  ctx,
    })
}

func AfterDo(ictx inst.HookContext, resp *mylib.Response, err error) {
    if !enabler.Enable() {
        return
    }
    span, ok := ictx.GetKeyData("span").(trace.Span)
    if !ok || span == nil {
        return
    }
    defer span.End()
    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
    }
}
```

### Semconv helpers

Put all attribute-building logic in a `semconv/` sub-package as pure functions that accept plain values and return `[]attribute.KeyValue`. This keeps the hook file focused on control flow and makes the semconv logic easy to unit test in isolation.

Always import from `go.opentelemetry.io/otel/semconv/v1.37.0` (check `.semconv-version` for the current pinned version). Run `make registry-check` to validate your semantic convention usage.

```go
// pkg/instrumentation/mylib/semconv/mylib.go

package semconv

import (
    "go.opentelemetry.io/otel/attribute"
    semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

func RequestAttrs(method, url string) []attribute.KeyValue {
    return []attribute.KeyValue{
        semconv.HTTPRequestMethodKey.String(method),
        semconv.URLFullKey.String(url),
    }
}
```

---

## Step 4: Write unit tests

Unit tests live next to the hook file and use no build tag. They test the Before/After hook functions in isolation by providing a mock `HookContext` and a real (but in-process) tracer.

### Using `insttest.MockHookContext`

The `insttest` package in `pkg/inst/insttest` provides `MockHookContext`, a shared implementation of `inst.HookContext` for testing. Use it instead of writing your own:

```go
import "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst/insttest"

ictx := insttest.NewMockHookContext(param0, param1)
```

`NewMockHookContext` accepts optional initial parameter values. The `Params` slice can be inspected or modified after the call to verify `ictx.SetParam` behaviour.

### Setting up an in-process tracer

```go
func setupTestTracer(t *testing.T) *tracetest.SpanRecorder {
    t.Helper()
    sr := tracetest.NewSpanRecorder()
    tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.TraceContext{})
    t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
    return sr
}
```

### Test cases to always include

| Test case | What to assert |
| :--- | :--- |
| Normal request creates a span | `len(sr.Ended()) == 1`, span name, span kind, required attributes |
| Instrumentation disabled via env var | `len(sr.Ended()) == 0` after calling Before+After |
| Error path | `span.Status()` is `Error`, `span.Events()` contains the error |
| Before stores data for After | After hook retrieves a non-nil span from `ictx.GetKeyData("span")` |
| Context propagation (client) | Outgoing carrier has `traceparent` header after Before hook |
| Context propagation (server) | Span has a remote parent when incoming carrier has `traceparent` |

### Example unit test skeleton

```go
// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package mylib

import (
    "context"
    "errors"
    "sync"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/propagation"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/sdk/trace/tracetest"
    "go.opentelemetry.io/otel/trace"

    "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst/insttest"
)

func setupTestTracer(t *testing.T) *tracetest.SpanRecorder {
    t.Helper()
    sr := tracetest.NewSpanRecorder()
    tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.TraceContext{})
    t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
    return sr
}

func TestBeforeDo_CreatesSpan(t *testing.T) {
    t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "mylib")
    initOnce = sync.Once{} // reset lazy init for each test
    sr := setupTestTracer(t)

    ictx := insttest.NewMockHookContext()
    req := &mylib.Request{Method: "GET", URL: "http://example.com"}
    BeforeDo(ictx, &mylib.Client{}, req)

    assert.Empty(t, sr.Ended(), "span must not be ended in Before hook")
    span, ok := ictx.Data["span"].(trace.Span)
    require.True(t, ok)
    require.NotNil(t, span)
}

func TestAfterDo_EndsSpan(t *testing.T) {
    t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "mylib")
    initOnce = sync.Once{}
    sr := setupTestTracer(t)

    ictx := insttest.NewMockHookContext()
    req := &mylib.Request{Method: "GET", URL: "http://example.com"}
    BeforeDo(ictx, &mylib.Client{}, req)
    AfterDo(ictx, &mylib.Response{StatusCode: 200}, nil)

    require.Len(t, sr.Ended(), 1)
}

func TestBeforeDo_DisabledNoSpan(t *testing.T) {
    t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "mylib")
    sr := setupTestTracer(t)

    ictx := insttest.NewMockHookContext()
    BeforeDo(ictx, &mylib.Client{}, &mylib.Request{Method: "GET", URL: "http://example.com"})
    AfterDo(ictx, nil, nil)

    assert.Empty(t, sr.Ended())
}

func TestAfterDo_RecordsError(t *testing.T) {
    t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "mylib")
    initOnce = sync.Once{}
    sr := setupTestTracer(t)

    ictx := insttest.NewMockHookContext()
    BeforeDo(ictx, &mylib.Client{}, &mylib.Request{Method: "GET", URL: "http://example.com"})
    AfterDo(ictx, nil, errors.New("connection refused"))

    require.Len(t, sr.Ended(), 1)
    span := sr.Ended()[0]
    assert.Equal(t, "Error", span.Status().Code.String())
}
```

Note: existing tests in `pkg/instrumentation/nethttp/client/` and `pkg/instrumentation/grpc/server/` still define a local `mockHookContext` type. New tests should use `insttest.NewMockHookContext` from `pkg/inst/insttest` instead — this avoids duplicating boilerplate.

### Running unit tests

```bash
go test -C pkg/instrumentation/<name> ./...
# or, for a client/server split:
go test -C pkg/instrumentation/<name>/client ./...
go test -C pkg/instrumentation/<name>/server ./...
```

### Limitation: struct field injection and unit tests

When a hook reads fields that are injected into a target struct via an `InstStructRule` (for example, `db.Endpoint` in `databasesql`), those fields do not exist at normal compile time. Unit tests that call such hook functions directly will therefore fail to compile.

For those hooks, limit unit test coverage to the logic paths that do not access injected fields, and rely on the integration test (Step 6) to cover the full execution path.

---

## Step 5: Create a test application

Integration and E2E tests build a real binary from a self-contained Go program in `test/apps/<name>/`. This binary must exercise the library you instrumented in a realistic way.

### `test/apps/<name>/main.go`

```go
// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a minimal <name> client for integration testing.
package main

import (
    "flag"
    "log"

    "example.com/mylib"
)

var addr = flag.String("addr", "http://localhost:8080", "target address")

func main() {
    flag.Parse()

    client := mylib.NewClient()
    resp, err := client.Do(mylib.NewRequest("GET", *addr))
    if err != nil {
        log.Fatalf("request failed: %v", err)
    }
    log.Printf("response: %d", resp.StatusCode)
}
```

Key requirements for a test app:
- Accept all connection parameters via command-line flags (e.g., `-addr`) so the integration test can parameterise it.
- Perform exactly the operation(s) you want to assert on — no more, no less.
- Exit with a non-zero code on failure.
- Be a standalone Go module (its own `go.mod`).

See `test/apps/httpclient/main.go` and `test/apps/grpcclient/main.go` for complete examples.

### `test/apps/<name>/go.mod`

```
module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/apps/<name>

go 1.25.0

require (
    example.com/mylib <version>
)
```

After creating the file:

```bash
make tidy/test-apps
```

---

## Step 6: Write the integration test

Integration tests live in `test/integration/` and must carry the build tag `//go:build integration`. Each test builds the test app with `otelc`, runs it, and asserts on the spans exported to an in-memory collector.

### Skeleton

```go
// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
    "testing"

    "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestMyLib(t *testing.T) {
    f := testutil.NewTestFixture(t)

    // Start any in-process service the app needs (e.g. httptest.Server, miniredis).
    // server := httptest.NewServer(...)
    // t.Cleanup(server.Close)

    f.BuildAndRun("mylib", "-addr="+server.URL)

    span := f.RequireSingleSpan()
    testutil.RequireMyLibSemconv(t, span, /* expected values */)
}
```

### `testutil.NewTestFixture`

`NewTestFixture` starts an in-memory OTLP collector and sets all required `OTEL_*` environment variables automatically. After calling `BuildAndRun`, spans are available via `f.Traces()`.

### Assertion helpers

`f.RequireSingleSpan()` is a convenience for "exactly one trace with exactly one span". For tests that produce multiple spans use `testutil.RequireSpan` with filter predicates:

```go
clientSpan := testutil.RequireSpan(t, f.Traces(),
    testutil.IsClient,
    testutil.HasAttributeContaining(string(semconv.URLFullKey), "/api"),
)
```

### Adding a semconv helper

If your instrumentation emits a new combination of attributes, add a `RequireXxxSemconv` function to `test/testutil/semconv.go`:

```go
// RequireMyLibSemconv verifies that a mylib span follows semantic conventions.
func RequireMyLibSemconv(t *testing.T, span ptrace.Span, method, url string) {
    t.Helper()
    RequireAttribute(t, span, string(semconv.HTTPRequestMethodKey), method)
    RequireAttribute(t, span, string(semconv.URLFullKey), url)
}
```

See the existing `RequireHTTPClientSemconv`, `RequireGRPCClientSemconv`, and `RequireDBClientSemconv` functions in that file as models.

### Running integration tests

Integration tests require that the `otelc` binary has been built first:

```bash
make build
make test-integration
```

---

## Step 7: Write an E2E test (when applicable)

E2E tests are only needed when the meaningful assertion involves **two or more instrumented processes** — for example, verifying that a trace context is correctly propagated from an HTTP client to an HTTP server, so both spans appear in the same trace.

If your integration test covers the full assertion (single process with an in-process dependency), you do not need an E2E test.

E2E tests live in `test/e2e/` and use the build tag `//go:build e2e`. See `test/e2e/http_test.go` for a complete example.

```go
//go:build e2e

package test

import (
    "testing"

    "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestMyLibE2E(t *testing.T) {
    f := testutil.NewTestFixture(t)

    f.BuildAndStart("mylibserver")
    testutil.WaitForTCP(t, "127.0.0.1:9090")

    f.BuildAndRun("mylibclient", "-addr", "http://127.0.0.1:9090")

    f.RequireTraceCount(1)
    f.RequireSpansPerTrace(2) // one client span + one server span

    clientSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsClient)
    serverSpan := testutil.RequireSpan(t, f.Traces(), testutil.IsServer)
    // assert attributes on clientSpan and serverSpan
    _ = clientSpan
    _ = serverSpan
}
```

---

## Step 8: Final checks

Before opening a pull request, run these commands from the repository root:

```bash
# Add Apache 2.0 license headers to any new .go files
make format/license

# Keep module graphs consistent
make go-mod-tidy
make crosslink

# Full build, format, lint, and unit test
make all
```

If you added new semconv usage, also run:

```bash
make registry-check
```

Then open a pull request following the guidelines in `CONTRIBUTING.md`.

---

## Reference

| Location | Purpose |
| :--- | :--- |
| `pkg/instrumentation/nethttp/client/` | Complete client hook with context injection, param mutation, error handling |
| `pkg/instrumentation/nethttp/server/` | Complete server hook with context extraction, response writer wrapping |
| `pkg/instrumentation/grpc/client/` | gRPC client hook using stats handler pattern |
| `pkg/instrumentation/grpc/server/` | gRPC server hook using stats handler pattern, with metrics |
| `pkg/instrumentation/databasesql/` | Struct field injection, multiple hooks in one YAML file |
| `pkg/instrumentation/redis/v9/` | Versioned target path (`v9` sub-directory) |
| `pkg/inst/insttest/mock_hook_context.go` | Shared `MockHookContext` for unit tests |
| `test/testutil/fixture.go` | `TestFixture`, `BuildAndRun`, `RequireSingleSpan` |
| `test/testutil/semconv.go` | Existing `RequireXxxSemconv` helpers |
| `test/e2e/http_test.go` | E2E test reference |
| `docs/rules.md` | Complete YAML rule type reference |
| `docs/testing.md` | Test strategy and when to use each category |
| `docs/semantic-conventions.md` | Semconv management workflow |
| `.semconv-version` | Currently pinned semconv version (`v1.37.0`) |
