// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInstrumentPhaseLogDelegators exercises the thin slog delegators on
// InstrumentPhase. They must forward to the underlying logger without panicking.
func TestInstrumentPhaseLogDelegators(t *testing.T) {
	ip := &InstrumentPhase{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	assert.NotPanics(t, func() {
		ip.Info("info", "k", "v")
		ip.Warn("warn", "k", "v")
		ip.Error("error", "k", "v")
		ip.Debug("debug", "k", "v")
	})
}
