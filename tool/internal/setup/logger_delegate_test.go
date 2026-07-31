// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSetupPhaseLogDelegators exercises the thin slog delegators on SetupPhase.
// They must forward to the underlying logger without panicking.
func TestSetupPhaseLogDelegators(t *testing.T) {
	sp := newTestSetupPhase()
	assert.NotPanics(t, func() {
		sp.Info("info", "k", "v")
		sp.Warn("warn", "k", "v")
		sp.Error("error", "k", "v")
		sp.Debug("debug", "k", "v")
	})
}
