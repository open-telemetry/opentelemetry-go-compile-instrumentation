// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"fmt"
	"log/slog"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

type InstrumentPreprocessor struct {
	logger *slog.Logger
}

func (*InstrumentPreprocessor) match(args []string) bool {
	// TODO: Implement task
	_ = args
	return false
}

func (*InstrumentPreprocessor) load() error {
	// TODO: Implement task
	return nil
}

func (*InstrumentPreprocessor) instrument(args []string) error {
	// TODO: Implement task
	_ = args
	return nil
}

func Toolexec(logger *slog.Logger, args []string) error {
	ip := &InstrumentPreprocessor{
		logger: logger,
	}
	// Load matched hook rules from setup phase
	err := ip.load()
	if err != nil {
		return err
	}
	// Check if the current package should be instrumented by matching the current
	// command with list of matched rules
	if ip.match(args) {
		// Okay, this package should be instrumented.
		err = ip.instrument(args)
		if err != nil {
			return err
		}
		return nil
	}
	// Otherwise, just run the command as is
	err = util.RunCmd(args...)
	if err != nil {
		return fmt.Errorf("failed to run command: %w", err)
	}
	return nil
}
