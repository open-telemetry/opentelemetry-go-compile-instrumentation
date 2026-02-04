// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/instrument"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/urfave/cli/v3"
	"golang.org/x/tools/go/packages"
)

type SetupPhase struct {
	logger     *slog.Logger
	ruleConfig string
}

func (sp *SetupPhase) Info(msg string, args ...any)  { sp.logger.Info(msg, args...) }
func (sp *SetupPhase) Error(msg string, args ...any) { sp.logger.Error(msg, args...) }
func (sp *SetupPhase) Warn(msg string, args ...any)  { sp.logger.Warn(msg, args...) }
func (sp *SetupPhase) Debug(msg string, args ...any) { sp.logger.Debug(msg, args...) }

// keepForDebug copies the file to the build temp directory for debugging
// Error is tolerated as it's not critical.
func (sp *SetupPhase) keepForDebug(srcPath string) {
	base := filepath.Base(srcPath)
	dstPath := filepath.Join(util.GetBuildTemp("debug"), "main", base)
	err := util.CopyFile(srcPath, dstPath)
	if err != nil {
		sp.Warn("failed to record added file", "path", srcPath, "error", err)
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

func getPackageDir(pkg *packages.Package) string {
	if len(pkg.GoFiles) > 0 {
		return filepath.Dir(pkg.GoFiles[0])
	}
	return ""
}

// Setup prepares the environment for further instrumentation.
func Setup(ctx context.Context, cmd *cli.Command) error {
	// The args are "go build ..."
	args := cmd.Args().Slice()
	args = append([]string{"go"}, args...)

	logger := util.LoggerFromContext(ctx)

	if isSetup() {
		logger.InfoContext(ctx, "Setup has already been completed, skipping setup.")
		return nil
	}

	sp := &SetupPhase{
		logger:     logger,
		ruleConfig: cmd.String("rules"),
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

	// Extract the embedded pkg module into local directory
	err = sp.extract()
	if err != nil {
		return err
	}

	// Match the hook code with these dependencies
	matched, err := sp.matchDeps(ctx, deps)
	if err != nil {
		return err
	}

	// Generate otel.runtime.go for all packages
	moduleDirs := make(map[string]bool)
	for _, pkg := range pkgs {
		if pkg.Module == nil {
			sp.Warn("skipping package without module", "package", pkg.PkgPath)
			continue
		}
		moduleDir := pkg.Module.Dir
		pkgDir := getPackageDir(pkg)
		if pkgDir == "" {
			pkgDir = moduleDir
		}
		// Introduce additional hook code by generating otel.runtime.go
		if err = sp.addDeps(matched, pkgDir); err != nil {
			return err
		}
		moduleDirs[moduleDir] = true
	}

	// Sync new dependencies to go.mod or vendor/modules.txt
	for moduleDir := range moduleDirs {
		if err = sp.syncDeps(ctx, matched, moduleDir); err != nil {
			return err
		}
	}

	// Write the matched hook to matched.txt for further instrument phase
	return sp.store(matched)
}

// setupGoCache creates a persistent GOCACHE in .otel-build/gocache if one isn't already set.
// This prevents cache pollution when modifying core packages via //go:linkname while
// allowing incremental builds to work properly.
func setupGoCache(ctx context.Context, env []string) ([]string, error) {
	if os.Getenv("GOCACHE") != "" {
		// User has explicitly set GOCACHE, respect it
		return env, nil
	}

	logger := util.LoggerFromContext(ctx)
	cacheDir := util.GetBuildTemp("gocache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, ex.Wrapf(err, "failed to create persistent GOCACHE")
	}

	env = append(env, "GOCACHE="+cacheDir)
	logger.DebugContext(ctx, "using GOCACHE", "path", cacheDir)
	return env, nil
}

// buildContextFlagsWithValue are go build flags that take a value and affect the build context.
//
//nolint:gochecknoglobals // private lookup table
var buildContextFlagsWithValue = map[string]bool{
	"-tags":    true, // Build tags
	"-mod":     true, // Module mode (vendor, mod, readonly)
	"-modfile": true, // Custom go.mod file
}

// buildContextBoolFlags are go build boolean flags that affect the build context.
//
//nolint:gochecknoglobals // private lookup table
var buildContextBoolFlags = map[string]bool{
	"-race":  true, // Race detector
	"-msan":  true, // Memory sanitizer
	"-cover": true, // Coverage
	"-asan":  true, // Address sanitizer
}

// extractBuildFlags extracts flags that affect the build context from the arguments.
// These flags need to be forwarded to `go list` when resolving import archives.
// Returns a slice of flag arguments preserving their original form.
//
// For boolean flags, the last occurrence wins. This correctly handles cases like:
//   - GOFLAGS=-race with -race=false on CLI (result: -race=false)
//   - -race -race=false (result: -race=false)
//   - -race=false -race (result: -race)
func extractBuildFlags(args []string) []string {
	var valueFlags []string
	type boolFlagValue struct {
		set   bool
		value bool
	}
	boolFlagState := make(map[string]boolFlagValue) // Track final state of boolean flags

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Handle -flag=value format
		if idx := strings.Index(arg, "="); idx > 0 {
			flagName := arg[:idx]
			flagValue := arg[idx+1:]

			// Handle value flags (e.g., -tags=foo, -mod=vendor)
			if buildContextFlagsWithValue[flagName] {
				valueFlags = append(valueFlags, arg)
				continue
			}

			// Handle boolean flags in =value format (e.g., -race=true, -race=false)
			// strconv.ParseBool accepts: 1, t, T, TRUE, true, True, 0, f, F, FALSE, false, False
			if buildContextBoolFlags[flagName] {
				if enabled, err := strconv.ParseBool(flagValue); err == nil {
					boolFlagState[flagName] = boolFlagValue{set: true, value: enabled} // Last value wins
				}
				// Parse error: ignore invalid value
				continue
			}
			continue
		}

		// Handle boolean flags like -race, -msan, -cover, -asan (implies true)
		if buildContextBoolFlags[arg] {
			boolFlagState[arg] = boolFlagValue{set: true, value: true}
			continue
		}

		// Handle -flag value format (for flags that take values)
		if buildContextFlagsWithValue[arg] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			valueFlags = append(valueFlags, arg, args[i+1])
			i++ // Skip the value
		}
	}

	// Collect boolean flags that are enabled (in deterministic order)
	var enabledBoolFlags []string
	for flag := range buildContextBoolFlags {
		if state, ok := boolFlagState[flag]; ok && state.set {
			if state.value {
				enabledBoolFlags = append(enabledBoolFlags, flag)
			} else {
				enabledBoolFlags = append(enabledBoolFlags, flag+"=false")
			}
		}
	}
	// Sort for deterministic output
	sort.Strings(enabledBoolFlags)

	// Combine: value flags first, then boolean flags
	return append(valueFlags, enabledBoolFlags...)
}

// BuildWithToolexec builds the project with the toolexec mode
func BuildWithToolexec(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
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

	// Extract and forward build flags that affect the build context
	// This ensures `go list` resolves archives matching the current build
	if buildFlags := extractBuildFlags(args); len(buildFlags) > 0 {
		encoded := util.EncodeBuildFlags(buildFlags)
		env = append(env, fmt.Sprintf("%s=%s", util.EnvOtelBuildFlags, encoded))
		logger.DebugContext(ctx, "forwarding build flags", "flags", buildFlags)
	}

	// Use a fresh GOCACHE to prevent cache pollution when modifying core packages
	env, err = setupGoCache(ctx, env)
	if err != nil {
		return err
	}

	return util.RunCmdWithEnv(ctx, env, newArgs...)
}

func GoBuild(ctx context.Context, cmd *cli.Command) error {
	logger := util.LoggerFromContext(ctx)

	// Clean up import tracking files from previous builds at the start
	// to prevent stale data from affecting this build.
	instrument.CleanupImportTrackingFiles()

	backupFiles := []string{"go.mod", "go.sum", "go.work", "go.work.sum"}
	err := util.BackupFile(backupFiles)
	if err != nil {
		logger.DebugContext(ctx, "failed to back up files", "error", err)
	}
	defer func() {
		var pkgs []*packages.Package
		pkgs, err = getBuildPackages(ctx, cmd.Args().Slice())
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

	err = Setup(ctx, cmd)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "Setup completed successfully")

	err = BuildWithToolexec(ctx, cmd)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "Instrumentation completed successfully")
	return nil
}
