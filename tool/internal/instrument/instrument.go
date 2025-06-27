// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"log/slog"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/util"
)

type InstrumentPreprocessor struct {
	logger *slog.Logger
}

func (ip *InstrumentPreprocessor) match(args []string) bool {
	// TODO: Implement task
	return false
}

func (ip *InstrumentPreprocessor) load() error {
	// TODO: Implement task
	return nil
}

func (ip *InstrumentPreprocessor) instrument(args []string) error {
	// TODO: Implement task
	return nil
}

func Toolexec(logger *slog.Logger, args []string) error {
	ip := &InstrumentPreprocessor{
		logger: logger,
	}
	// Load matched hook code from setup phase
	err := ip.load()
	if err != nil {
		return err
	}
	// Check if the current package should be instrumented by matching the current
	// command with list of matched hook
	if ip.match(args) {
		// Okay, this package should be instrumented.
		err := ip.instrument(args)
		if err != nil {
			return err
		}
		return nil
	}
	// Otherwise, just run the command as is
	return util.RunCmd(args...)
}
