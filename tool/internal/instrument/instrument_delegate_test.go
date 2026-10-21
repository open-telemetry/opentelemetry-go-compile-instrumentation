// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInstrumentPhaseLogDelegators exercises the thin slog delegators on
// instrumentPhase. They must forward to the underlying logger without panicking.
func TestInstrumentPhaseLogDelegators(t *testing.T) {
	ip := &instrumentPhase{logger: slog.New(slog.DiscardHandler)}
	assert.NotPanics(t, func() {
		ip.Info("info", "k", "v")
		ip.Warn("warn", "k", "v")
		ip.Error("error", "k", "v")
		ip.Debug("debug", "k", "v")
	})
}
