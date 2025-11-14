// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nethttp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildNetHttpServerOtelInstrumenter(t *testing.T) {
	instrumenter := BuildNetHttpServerOtelInstrumenter()

	// Verify instrumenter is created successfully
	require.NotNil(t, instrumenter, "instrumenter should not be nil")

	// The instrumenter should be a PropagatingFromUpstreamInstrumenter
	// This validates the basic construction succeeded
}

func TestServerInstrumenterWithEnabledFlag(t *testing.T) {
	// Build instrumenter (uses environment variable OTEL_INSTRUMENTATION_NETHTTP_ENABLED)
	instrumenter := BuildNetHttpServerOtelInstrumenter()
	require.NotNil(t, instrumenter)

	// The instrumenter should exist regardless of enabler state
	// The actual enabling/disabling happens in the hooks
}
