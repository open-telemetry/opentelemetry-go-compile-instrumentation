// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"log/slog"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

type InstrumentPhase struct {
	logger *slog.Logger
}

func Toolexec(logger *slog.Logger, args []string) error {
	ip := &InstrumentPhase{
		logger: logger,
	}
	// Load matched hook rules from setup phase
	err := ip.load()
	if err != nil {
		return ex.Error(err)
	}
	// Check if the current package should be instrumented by matching the current
	// command with list of matched rules
	if ip.match(args) {
		// Okay, this package should be instrumented.
		err = ip.instrument(args)
		if err != nil {
			return ex.Error(err)
		}
		return nil
	}
	// Otherwise, just run the command as is
	err = util.RunCmd(args...)
	if err != nil {
		return ex.Errorf(err, "failed to run command")
	}
	return nil
}
