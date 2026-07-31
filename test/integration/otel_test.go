//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"testing"

	"go.opentelemetry.io/otelc/test/testutil"
)

// TestOtelTraceWithoutSDK verifies that the trace instrumentation can be
// applied without the SDK trace instrumentation.
//
// The test application imports only go.opentelemetry.io/otel/trace. The
// build should succeed, and trace.SpanFromContext(context.Background())
// should return the original no-op span rather than attempting a GLS
// lookup.
func TestOtelTraceWithoutSDK(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "oteltrace", "go", "build", "-a")

	f := testutil.NewTestFixture(t)
	_ = f.Run("oteltrace")
}
