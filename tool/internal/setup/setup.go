// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"golang.org/x/tools/go/packages"
)

type SetupPhase struct {
	logger *slog.Logger
}

func (sp *SetupPhase) Info(msg string, args ...any)  { sp.logger.Info(msg, args...) }
func (sp *SetupPhase) Error(msg string, args ...any) { sp.logger.Error(msg, args...) }
func (sp *SetupPhase) Warn(msg string, args ...any)  { sp.logger.Warn(msg, args...) }
func (sp *SetupPhase) Debug(msg string, args ...any) { sp.logger.Debug(msg, args...) }

// keepForDebug copies the file to the build temp directory for debugging
// Error is tolerated as it's not critical.
func (sp *SetupPhase) keepForDebug(name string) {
	dstFile := filepath.Join(util.GetBuildTemp("debug"), "main", name)
	err := util.CopyFile(name, dstFile)
	if err != nil {
		sp.Warn("failed to record added file", "file", name, "error", err)
	}
}

// This function can be used to check if the setup has been completed.
func isSetup() bool {
	// TODO: Implement Task
	return false
}

// flagsWithPathValues contains flags that accept a value from "go build" command.
//
//nolint:gochecknoglobals // private lookup table
var flagsWithPathValues = map[string]bool{
	"-C":             true,
	"-o":             true,
	"-p":             true,
	"-covermode":     true,
	"-coverpkg":      true,
	"-asmflags":      true,
	"-buildmode":     true,
	"-buildvcs":      true,
	"-compiler":      true,
	"-gccgoflags":    true,
	"-gcflags":       true,
	"-installsuffix": true,
	"-ldflags":       true,
	"-mod":           true,
	"-modfile":       true,
	"-overlay":       true,
	"-pgo":           true,
	"-pkgdir":        true,
	"-tags":          true,
	"-toolexec":      true,
}

// GetBuildPackages loads all packages from the go build command arguments.
// Returns a list of loaded packages. If no package patterns are found in args,
// defaults to loading the current directory package.
// The args parameter should be the go build command arguments (e.g., ["build", "-a", "./cmd"]).
// Returns an error if package loading fails or if invalid patterns are provided.
// For example:
//   - args ["build", "-a", "./cmd"] returns packages for "./cmd"
//   - args ["build", "-a", "cmd"] returns packages for the "cmd" package in the module
//   - args ["build", "-a", ".", "./cmd"] returns packages for both "." and "./cmd"
//   - args ["build"] returns packages for "."
func getBuildPackages(ctx context.Context, args []string) ([]*packages.Package, error) {
	logger := util.LoggerFromContext(ctx)

	buildPkgs := make([]*packages.Package, 0)
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedModule,
	}
	found := false
	for i := len(args) - 1; i >= 0; i-- {
		arg := args[i]

		// If preceded by a flag that takes a path value, this is a flag value
		// We want to avoid scenarios like "go build -o ./tmp ./app" where tmp also contains Go files,
		// as it would be treated as a package.
		if i > 0 && flagsWithPathValues[args[i-1]] {
			break
		}

		// If we hit a flag, stop. Packages come after all flags
		// go build [-o output] [build flags] [packages]
		if strings.HasPrefix(arg, "-") || arg == "go" || arg == "build" || arg == "install" {
			break
		}

		pkgs, err := packages.Load(cfg, arg)
		if err != nil {
			return nil, ex.Wrapf(err, "failed to load packages for pattern %s", arg)
		}
		for _, pkg := range pkgs {
			if pkg.Errors != nil || pkg.Module == nil {
				logger.DebugContext(ctx, "skipping package", "pattern", arg, "errors", pkg.Errors, "module", pkg.Module)
				continue
			}
			buildPkgs = append(buildPkgs, pkg)
			found = true
		}
	}

	if !found {
		var err error
		buildPkgs, err = packages.Load(cfg, ".")
		if err != nil {
			return nil, ex.Wrapf(err, "failed to load packages for pattern .")
		}
	}
	return buildPkgs, nil
}

// Setup prepares the environment for further instrumentation.
func Setup(ctx context.Context, args []string) error {
	logger := util.LoggerFromContext(ctx)

	if isSetup() {
		logger.InfoContext(ctx, "Setup has already been completed, skipping setup.")
		return nil
	}

	sp := &SetupPhase{
		logger: logger,
	}

	// Introduce additional hook code by generating otel.runtime.go
	// Use GetPackage to determine the build target directory
	pkgs, err := getBuildPackages(ctx, args)
	if err != nil {
		return err
	}

	// Find all dependencies of the project being build
	deps, err := sp.findDeps(ctx, args)
	if err != nil {
		return err
	}
	// Match the hook code with these dependencies
	matched, err := sp.matchDeps(ctx, deps)
	if err != nil {
		return err
	}
	for _, pkg := range pkgs {
		// Introduce additional hook code by generating otel.runtime.go
		if err = sp.addDeps(matched, pkg.Dir); err != nil {
			return err
		}
	}
	// Extract the embedded instrumentation modules into local directory
	err = sp.extract()
	if err != nil {
		return err
	}
	// Sync new dependencies to go.mod or vendor/modules.txt
	err = sp.syncDeps(ctx, matched)
	if err != nil {
		return err
	}
	// Write the matched hook to matched.txt for further instrument phase
	return sp.store(matched)
}

// BuildWithToolexec builds the project with the toolexec mode
func BuildWithToolexec(ctx context.Context, args []string) error {
	logger := util.LoggerFromContext(ctx)

	// Add -toolexec=otel to the original build command and run it
	execPath, err := os.Executable()
	if err != nil {
		return ex.Wrapf(err, "failed to get executable path")
	}
	insert := "-toolexec=" + execPath + " toolexec"
	const additionalCount = 2
	newArgs := make([]string, 0, len(args)+additionalCount) // Avoid in-place modification
	// Add "go build"
	newArgs = append(newArgs, "go")
	newArgs = append(newArgs, args[:1]...)
	// Add "-work" to give us a chance to debug instrumented code if needed
	newArgs = append(newArgs, "-work")
	// Add "-toolexec=..."
	newArgs = append(newArgs, insert)
	// TODO: We should support incremental build in the future, so we don't need
	// to force rebuild here.
	// Add "-a" to force rebuild
	newArgs = append(newArgs, "-a")
	// Add the rest
	newArgs = append(newArgs, args[1:]...)
	logger.InfoContext(ctx, "Running go build with toolexec", "args", newArgs)

	// Tell the sub-process the working directory
	env := os.Environ()
	pwd := util.GetOtelWorkDir()
	util.Assert(pwd != "", "invalid working directory")
	env = append(env, fmt.Sprintf("%s=%s", util.EnvOtelWorkDir, pwd))

	return util.RunCmdWithEnv(ctx, env, newArgs...)
}

func GoBuild(ctx context.Context, args []string) error {
	logger := util.LoggerFromContext(ctx)
	backupFiles := []string{"go.mod", "go.sum", "go.work", "go.work.sum"}
	err := util.BackupFile(backupFiles)
	if err != nil {
		logger.DebugContext(ctx, "failed to back up files", "error", err)
	}
	defer func() {
		var pkgs []*packages.Package
		pkgs, err = getBuildPackages(ctx, args)
		if err != nil {
			logger.DebugContext(ctx, "failed to get build packages", "error", err)
		}
		for _, pkg := range pkgs {
			if err = os.RemoveAll(filepath.Join(pkg.Dir, OtelRuntimeFile)); err != nil {
				logger.DebugContext(ctx, "failed to remove generated file from package",
					"file", filepath.Join(pkg.Dir, OtelRuntimeFile), "error", err)
			}
		}
		if err = os.RemoveAll(unzippedPkgDir); err != nil {
			logger.DebugContext(ctx, "failed to remove unzipped pkg", "error", err)
		}
		if err = util.RestoreFile(backupFiles); err != nil {
			logger.DebugContext(ctx, "failed to restore files", "error", err)
		}
	}()

	err = Setup(ctx, os.Args[1:])
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "Setup completed successfully")

	err = BuildWithToolexec(ctx, args)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "Instrumentation completed successfully")
	return nil
}
