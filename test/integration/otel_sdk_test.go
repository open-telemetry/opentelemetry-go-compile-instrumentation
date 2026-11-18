//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

// TestOtelImplicitRulesWithoutOtelImports verifies #1712 for an application
// that imports no OpenTelemetry package at all. The front handler calls a
// second local handler with plain http.Get, which uses context.Background().
// The client span must join the server span's trace, because otelc links the
// OTel API, the trace API, and the SDK trace package through pkg/runtime.
func TestOtelImplicitRulesWithoutOtelImports(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "otelnoimport", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	port := testutil.FreePort(t)

	f.Start("otelnoimport", fmt.Sprintf("-port=%d", port))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/front", port))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// We expect exactly 1 trace with 3 spans:
	// 1. HTTP server (front handler, trace root)
	// 2. HTTP client (front handler to api handler, started from context.Background())
	// 3. HTTP server (api handler)
	f.WaitForSpans(3)
	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(3)

	server := testutil.RequireSpan(t, f.Traces(), testutil.IsServer, testutil.HasName("GET /front"))
	client := testutil.RequireSpan(t, f.Traces(), testutil.IsClient)
	apiServer := testutil.RequireSpan(t, f.Traces(), testutil.IsServer, testutil.HasName("GET /api"))

	// The client span runs without the request context. It must still be a
	// child of the front server span, in the same trace.
	require.Equal(t, server.TraceID(), client.TraceID(),
		"the client span must join the server span's trace")
	require.Equal(t, server.SpanID(), client.ParentSpanID(),
		"the front server span must be the parent of the client span")
	require.Equal(t, client.SpanID(), apiServer.ParentSpanID(),
		"the client span must be the parent of the api server span")
	require.True(t, server.ParentSpanID().IsEmpty(), "the front server span must be the trace root")
}

// TestOtelSDKSpanFromContext verifies that trace.SpanFromContext returns
// the active span from GLS when called with context.Background().
// This tests the full integration of:
//   - runtime GLS fields (otel_trace_context)
//   - otel SDK trace context injection (newRecordingSpanOnExit adds span to GLS)
//   - otel trace SpanFromContext hook (spanFromContextOnExit reads from GLS)
//   - net/http server instrumentation (creates the span)
func TestOtelSDKSpanFromContext(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "otelsdk", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	f.SetEnv("OTEL_GLS_MAX_SPANS", "3")

	var output string
	defer func() {
		if t.Failed() {
			t.Logf("otelsdk output:\n%s", output)
		}
	}()
	output = f.Run("otelsdk")
	require.Contains(t, output, "OTEL_SDK_TEST: span valid",
		"SpanFromContext(context.Background()) should return a valid span from GLS")
	require.Contains(t, output, "traceID=")
	require.Contains(t, output, "spanID=")
	require.Contains(t, output, "OTEL_SDK_WORKER: stale span=false")
	require.Contains(t, output, "OTEL_SDK_COMPACT: admitted=true",
		"ended spans below the active span should not consume the GLS limit")

	workerSpan := testutil.RequireSpan(t, f.Traces(), testutil.HasName("worker-span"))
	require.True(t, workerSpan.ParentSpanID().IsEmpty(),
		"a reused worker must not keep an ended span as its parent")

	f = testutil.NewTestFixture(t)
	f.SetEnv("OTEL_TRACES_SAMPLER", "always_off")
	output = f.Run("otelsdk")
	require.Contains(t, output, "OTEL_SDK_TEST: span valid",
		"an active non-recording span should propagate through GLS")
	require.Contains(t, output, "OTEL_SDK_WORKER: stale span=false",
		"an ended non-recording span should be removed from GLS")

	// The next two scenarios reuse the same build (see testutil.Build above)
	// and only vary environment at run time, so they must not spawn their own
	// parallel builds against this app's shared output binary.

	// Once a goroutine's live (unended) span count reaches OTEL_GLS_MAX_SPANS,
	// the drop must be observable via a debug log line instead of failing
	// silently, and implicit lookups must keep returning the last
	// successfully tracked span rather than something undefined.
	//
	// The cap is 2, not 1: the net/http server instrumentation has already
	// pushed a span for the request onto the handler's goroutine, so 2 is what
	// leaves room for "live-first" and makes "live-second" the span that gets
	// dropped (see liveCapHandler).
	f = testutil.NewTestFixture(t)
	f.SetEnv("OTELSDK_PATHS", "livecap")
	f.SetEnv("OTEL_GLS_MAX_SPANS", "2")
	f.SetEnv("OTEL_LOG_LEVEL", "debug")
	output = f.Run("otelsdk")
	require.Contains(t, output, "OTEL_SDK_LIVECAP: stale-parent-is-first=true",
		"live-second must not be admitted once the server span and live-first fill the "+
			"OTEL_GLS_MAX_SPANS=2 stack")
	require.Contains(t, output, "GLS span stack at capacity, span not tracked for implicit propagation",
		"the dropped span must be logged at debug level, not silently discarded")

	// Once the shared spanStates tracker reaches OTEL_GLS_MAX_SPAN_STATES,
	// eviction must always drop the oldest entry deterministically and never
	// touch a younger, still-live entry.
	f = testutil.NewTestFixture(t)
	f.SetEnv("OTELSDK_PATHS", "evict")
	f.SetEnv("OTEL_GLS_MAX_SPAN_STATES", "2")
	f.SetEnv("OTEL_LOG_LEVEL", "debug")
	output = f.Run("otelsdk")
	require.Contains(t, output, "OTEL_SDK_EVICT: span-a valid before eviction=true")
	require.Contains(t, output, "OTEL_SDK_EVICT: span-a evicted=true",
		"span-a is the oldest tracked entry and must be evicted once the shared state map is full")
	require.Contains(t, output, "OTEL_SDK_EVICT: span-b survived=true",
		"span-b is younger than span-a and must never be evicted while span-a is still the oldest entry")
	require.Contains(t, output, "GLS span state tracker at capacity, evicting oldest entry",
		"eviction must be logged at debug level")
}
