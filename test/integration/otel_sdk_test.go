//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

// TestOtelSDKSpanFromContext verifies that trace.SpanFromContext returns
// the active span from GLS when called with context.Background().
// This tests the full integration of:
//   - runtime GLS fields (otel_trace_context)
//   - otel SDK trace context injection (newRecordingSpanOnExit adds span to GLS)
//   - otel trace SpanFromContext hook (spanFromContextOnExit reads from GLS)
//   - net/http server instrumentation (creates the span)
func TestOtelSDKSpanFromContext(t *testing.T) {
	f := testutil.NewTestFixture(t)

	var output string
	defer func() {
		if t.Failed() {
			t.Logf("otelsdk output:\n%s", output)
		}
	}()
	output = f.BuildAndRun("otelsdk")
	require.Contains(t, output, "OTEL_SDK_TEST: span valid",
		"SpanFromContext(context.Background()) should return a valid span from GLS")
	require.Contains(t, output, "traceID=")
	require.Contains(t, output, "spanID=")
}
