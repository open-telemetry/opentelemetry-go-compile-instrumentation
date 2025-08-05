// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"log/slog"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

type InstrumentPhase struct {
	logger *slog.Logger
}

func (ip *InstrumentPhase) Info(msg string, args ...any)  { ip.logger.Info(msg, args...) }
func (ip *InstrumentPhase) Error(msg string, args ...any) { ip.logger.Error(msg, args...) }
func (ip *InstrumentPhase) Warn(msg string, args ...any)  { ip.logger.Warn(msg, args...) }
func (ip *InstrumentPhase) Debug(msg string, args ...any) { ip.logger.Debug(msg, args...) }

func Toolexec(logger *slog.Logger, args []string) error {
	ip := &InstrumentPhase{
		logger: logger,
	}
	// Load matched hook rules from setup phase
	rules, err := ip.load()
	if err != nil {
		return err
	}
	// Check if the current package should be instrumented by matching the current
	// command with list of matched rules
	if ip.match(args, rules) {
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
		return err
	}
	return nil
}
