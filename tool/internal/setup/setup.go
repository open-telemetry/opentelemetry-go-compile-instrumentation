// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

type SetupProcessor struct {
	logger *slog.Logger
}

func (sp *SetupProcessor) Info(msg string, args ...any)  { sp.logger.Info(msg, args...) }
func (sp *SetupProcessor) Error(msg string, args ...any) { sp.logger.Error(msg, args...) }
func (sp *SetupProcessor) Warn(msg string, args ...any)  { sp.logger.Warn(msg, args...) }
func (sp *SetupProcessor) Debug(msg string, args ...any) { sp.logger.Debug(msg, args...) }

func (*SetupProcessor) store(matched []*rule.InstRule) error {
	f := util.GetBuildTemp("matched.txt")
	file, err := os.Create(f)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", f, err)
	}
	defer file.Close()
	for _, r := range matched {
		_, err = fmt.Fprintf(file, "%s\n", r.Name)
		if err != nil {
			return fmt.Errorf("failed to write to file %s: %w", f, err)
		}
	}
	return nil
}

// This function can be used to check if the setup has been completed.
func isSetup() bool {
	// TODO: Implement Task
	return false
}

// This function is intended to prepare the environment for instrumentation.
func Setup(logger *slog.Logger) error {
	if isSetup() {
		logger.Info("Setup has already been completed, skipping setup.")
		return nil
	}

	sp := &SetupProcessor{
		logger: logger,
	}
	// Find all dependencies of the project being build
	deps, err := sp.findDeps(os.Args[1:])
	if err != nil {
		return err
	}
	// Match the hook code with these dependencies
	matched, err := sp.matchedDeps(deps)
	if err != nil {
		return err
	}
	// Introduce additional hook code by generating otel.instrumentation.go
	err = sp.addDeps(deps)
	if err != nil {
		return err
	}
	// Run `go mod tidy` to refresh dependencies
	err = sp.refreshDeps()
	if err != nil {
		return err
	}
	// Write the matched hook to matched.txt for further instrument phase
	err = sp.store(matched)
	if err != nil {
		return err
	}
	sp.Info("Setup completed successfully")
	return nil
}
