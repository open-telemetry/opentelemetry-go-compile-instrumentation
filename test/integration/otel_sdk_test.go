//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

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
}
