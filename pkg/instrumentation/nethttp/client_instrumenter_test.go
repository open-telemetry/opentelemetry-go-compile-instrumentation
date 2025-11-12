// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nethttp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildNetHttpClientOtelInstrumenter(t *testing.T) {
	instrumenter := BuildNetHttpClientOtelInstrumenter()

	// Verify instrumenter is created successfully
	require.NotNil(t, instrumenter, "client instrumenter should not be nil")
}

func TestClientInstrumenterWithEnabledFlag(t *testing.T) {
	instrumenter := BuildNetHttpClientOtelInstrumenter()
	require.NotNil(t, instrumenter)
}
