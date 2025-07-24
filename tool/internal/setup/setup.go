// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

type SetupPhase struct {
	logger *slog.Logger
}

func (sp *SetupPhase) Info(msg string, args ...any)  { sp.logger.Info(msg, args...) }
func (sp *SetupPhase) Error(msg string, args ...any) { sp.logger.Error(msg, args...) }
func (sp *SetupPhase) Warn(msg string, args ...any)  { sp.logger.Warn(msg, args...) }
func (sp *SetupPhase) Debug(msg string, args ...any) { sp.logger.Debug(msg, args...) }

// recordModified copies the file to the build temp directory for debugging
// Error is tolerated as it's not critical.
func (sp *SetupPhase) recordModified(name string) {
	dstFile := filepath.Join(util.GetBuildTemp("modified"), name)
	err := util.CopyFile(name, dstFile)
	if err != nil {
		sp.Warn("failed to copy file", "file", name, "error", err)
	}
}

func (*SetupPhase) store(matched []*rule.InstRule) error {
	f := util.GetBuildTemp("matched.txt")
	file, err := os.Create(f)
	if err != nil {
		return ex.Errorf(err, "failed to create file %s", f)
	}
	defer file.Close()
	for _, r := range matched {
		_, err = fmt.Fprintf(file, "%s\n", r.Name)
		if err != nil {
			return ex.Errorf(err, "failed to write to file %s", f)
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

	sp := &SetupPhase{
		logger: logger,
	}
	// Find all dependencies of the project being build
	deps, err := sp.findDeps(os.Args[1:])
	if err != nil {
		return ex.Error(err)
	}
	// Match the hook code with these dependencies
	matched, err := sp.matchedDeps(deps)
	if err != nil {
		return ex.Error(err)
	}
	// Introduce additional hook code by generating otel.instrumentation.go
	err = sp.addDeps(matched)
	if err != nil {
		return ex.Error(err)
	}
	// Sync new dependencies to go.mod or vendor/modules.txt
	err = sp.syncDeps(matched)
	if err != nil {
		return ex.Error(err)
	}
	// Write the matched hook to matched.txt for further instrument phase
	err = sp.store(matched)
	if err != nil {
		return ex.Error(err)
	}
	sp.Info("Setup completed successfully")
	return nil
}
