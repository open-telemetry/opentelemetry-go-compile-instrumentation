// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
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

// This function can be used to check if the setup has been completed.
func isSetup() bool {
	// TODO: Implement Task
	return false
}

// Setup prepares the environment for further instrumentation.
func Setup(ctx context.Context) error {
	logger := util.LoggerFromContext(ctx)

	if isSetup() {
		logger.InfoContext(ctx, "Setup has already been completed, skipping setup.")
		return nil
	}

	sp := &SetupPhase{
		logger: logger,
	}
	// Find all dependencies of the project being build
	deps, err := sp.findDeps(ctx, os.Args[1:])
	if err != nil {
		return err
	}
	// Match the hook code with these dependencies
	matched, err := sp.matchedDeps(deps)
	if err != nil {
		return err
	}
	// Introduce additional hook code by generating otel.instrumentation.go
	err = sp.addDeps(matched)
	if err != nil {
		return err
	}
	// Sync new dependencies to go.mod or vendor/modules.txt
	err = sp.syncDeps(ctx, matched)
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

// BuildWithToolexec builds the project with the toolexec mode
func BuildWithToolexec(ctx context.Context, args []string) error {
	logger := util.LoggerFromContext(ctx)

	// Add -toolexec=otel to the original build command and run it
	execPath, err := os.Executable()
	if err != nil {
		return ex.Errorf(err, "failed to get executable path")
	}
	insert := "-toolexec=" + execPath + " toolexec"
	const additionalCount = 2
	newArgs := make([]string, 0, len(args)+additionalCount) // Avoid in-place modification
	newArgs = append(newArgs, "go")
	newArgs = append(newArgs, args[:2]...) // Add "go build"
	newArgs = append(newArgs, insert)      // Add "-toolexec=..."
	newArgs = append(newArgs, args[2:]...) // Add the rest
	logger.InfoContext(ctx, "Running go build with toolexec", "args", newArgs)

	// Tell the sub-process the working directory
	env := os.Environ()
	pwd := util.GetOtelWorkDir()
	util.Assert(pwd != "", "invalid working directory")
	env = append(env, fmt.Sprintf("%s=%s", util.EnvOtelWorkDir, pwd))

	err = util.RunCmdWithEnv(ctx, env, newArgs...)
	if err != nil {
		return err
	}
	return nil
}
