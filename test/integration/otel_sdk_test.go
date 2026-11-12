//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

// TestOtelSDKImplicitRulesWithoutAppSDK verifies #1712: the SDK trace
// rules apply even when the application imports only the OTel API. The
// test application starts a span from context.Background() inside a traced
// server handler. Without the implicit SDK instrumentation that span starts
// a new trace, because nothing propagates the server span through GLS.
func TestOtelSDKImplicitRulesWithoutAppSDK(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "otelsdkimplicit", "go", "build", "-a")

	f := testutil.NewTestFixture(t)

	var output string
	defer func() {
		if t.Failed() {
			t.Logf("otelsdkimplicit output:\n%s", output)
		}
	}()
	output = f.Run("otelsdkimplicit", fmt.Sprintf("-port=%d", testutil.FreePort(t)))
	require.Contains(t, output, "OTEL_SDK_IMPLICIT: parent-valid=true",
		"SpanFromContext(context.Background()) should return the server span from GLS")
	require.Contains(t, output, "same-trace=true",
		"a span started from context.Background() must join the server span's trace")

	f.WaitForSpans(3)

	server := testutil.RequireSpan(t, f.Traces(), testutil.IsServer, testutil.HasName("GET /otel"))
	child := testutil.RequireSpan(t, f.Traces(), testutil.HasName("bg-child-span"))
	require.Equal(t, server.TraceID(), child.TraceID(),
		"the span started from context.Background() must share the server span's trace")
	require.Equal(t, server.SpanID(), child.ParentSpanID(),
		"the server span must be the parent of the span started from context.Background()")
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
