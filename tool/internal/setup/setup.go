// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/instrument"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/pkgload"
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

const commandLineArgumentsPackage = "command-line-arguments"

// consumeCFlagPositional consumes -C (or --C) only when it appears as the
// first argument in args, matching Go toolchain semantics (see handleChdirFlag).
// Both single-dash (-C) and double-dash (--C) forms are supported, as is the
// equals form (-C=dir / --C=dir).
// Returns ("", args) if -C is not present at position 0.
func consumeCFlagPositional(args []string) (string, []string) {
	if len(args) == 0 {
		return "", args
	}
	if strings.HasPrefix(args[0], "-C=") {
		return strings.TrimPrefix(args[0], "-C="), args[1:]
	}
	if strings.HasPrefix(args[0], "--C=") {
		return strings.TrimPrefix(args[0], "--C="), args[1:]
	}
	if (args[0] == "-C" || args[0] == "--C") && len(args) > 1 {
		return args[1], args[2:]
	}
	return "", args
}

// GetBuildPackages loads all packages from the otelc go build/install or otelc setup command arguments.
// Returns a list of loaded packages. If no package patterns are found in args,
// defaults to loading the current directory package.
// The args parameter should be the go build/install command arguments (e.g., ["-a", "./cmd"]).
// Returns an error if package loading fails or if invalid patterns are provided.
// For example:
//   - args ["-a", "./cmd"] returns packages for "./cmd"
//   - args ["-a", "cmd"] returns packages for the "cmd" package in the module
//   - args ["-a", ".", "./cmd"] returns packages for both "." and "./cmd"
//   - args [] returns packages for "."
func getBuildPackages(ctx context.Context, args []string) ([]*packages.Package, error) {
	logger := util.LoggerFromContext(ctx)
	mode := packages.NeedName | packages.NeedFiles | packages.NeedModule

	pkgTargets, fileTargets, err := splitBuildTargets(args)
	if err != nil {
		return nil, ex.Wrapf(err, "splitting build targets")
	}

	var (
		pkgs    []*packages.Package
		loadErr error
	)
	switch {
	case len(fileTargets) > 0:
		pkgs, loadErr = pkgload.LoadPackages(ctx, mode, nil, fileTargets...)
		if loadErr != nil {
			return nil, ex.Wrapf(loadErr, "failed to load packages for files %v", fileTargets)
		}

		if len(pkgs) > 1 {
			return nil, ex.New("multiple packages found for file targets")
		}
	case len(pkgTargets) > 0:
		pkgs, loadErr = pkgload.LoadPackages(ctx, mode, nil, pkgTargets...)
		if loadErr != nil {
			return nil, ex.Wrapf(loadErr, "failed to load packages for patterns %v", pkgTargets)
		}
	default:
		pkgs, loadErr = pkgload.LoadPackages(ctx, mode, nil, ".")
		if loadErr != nil {
			return nil, ex.Wrapf(loadErr, "failed to load packages for pattern .")
		}
	}

	buildPkgs := make([]*packages.Package, 0, len(pkgs))
	for _, pkg := range pkgs {
		// file-based builds use synthetic "command-line-arguments" packages
		if len(pkg.Errors) > 0 || (pkg.Module == nil && pkg.PkgPath != commandLineArgumentsPackage) {
			logger.DebugContext(ctx, "skipping package", "name", pkg.Name, "errors", pkg.Errors, "args", args)
			continue
		}

		buildPkgs = append(buildPkgs, pkg)
	}

	if len(buildPkgs) == 0 {
		return nil, ex.New("no valid packages found in build targets")
	}

	return buildPkgs, nil
}

//nolint:revive // if we add named returns then nonamedreturns will complain
func splitBuildTargets(args []string) ([]string, []string, error) {
	var pkgs, files []string

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
		if strings.HasPrefix(arg, "-") {
			break
		}

		if filepath.Ext(arg) == ".go" {
			files = append(files, arg)
		} else {
			pkgs = append(pkgs, arg)
		}
	}

	if len(files) > 0 && len(pkgs) > 0 {
		return nil, nil, ex.New("cannot mix .go files and packages")
	}

	if len(files) > 0 {
		// files are collected in reverse order due to reverse argument traversal.
		// files[0] is therefore the last .go file from the original CLI args.
		dir, err := filepath.Abs(filepath.Dir(files[0]))
		if err != nil {
			return nil, nil, ex.Wrapf(err, "failed to get absolute path for directory containing files")
		}

		for _, f := range files[1:] {
			fdir, err2 := filepath.Abs(filepath.Dir(f))
			if err2 != nil {
				return nil, nil, ex.Wrapf(err2, "failed to get absolute path for directory containing file %s", f)
			}

			if fdir != dir {
				return nil, nil, ex.New("named files must all be in one directory")
			}
		}
	}

	return pkgs, files, nil
}

func getPackageDir(pkg *packages.Package) string {
	if len(pkg.GoFiles) > 0 {
		return filepath.Dir(pkg.GoFiles[0])
	}
	return ""
}

// Setup prepares the environment for further instrumentation.
func Setup(ctx context.Context, cmd *cli.Command) error {
	// Setup is invoked in three shapes; -C can appear in any of these positions:
	//   1. `otelc setup -C ./dir`              -- -C at position 0
	//   2. `otelc go build -C ./dir ...`       -- -C after build/install
	//   3. `otelc go -C ./dir build ...`       -- -C before build/install
	args := cmd.Args().Slice()
	if cmd.Name == "go" {
		// Strip the -C-before-build form first so the build/install element is
		// at a known position.
		if dir, rest := consumeCFlagPositional(args); dir != "" {
			if err := os.Chdir(dir); err != nil {
				return ex.Wrapf(err, "changing to -C directory %s", dir)
			}
			args = rest
		}
		if len(args) > 0 {
			args = args[1:] // trim build/install
		}
	}

	// Honor -C as the next positional arg, matching Go toolchain semantics
	// (see handleChdirFlag). os.Chdir does not affect the parent shell.
	if dir, rest := consumeCFlagPositional(args); dir != "" {
		if err := os.Chdir(dir); err != nil {
			return ex.Wrapf(err, "changing to -C directory %s", dir)
		}
		args = rest
	}

	logger := util.LoggerFromContext(ctx)

	if isSetup() {
		logger.InfoContext(ctx, "Setup has already been completed, skipping setup.")
		return nil
	}

	// Back up go.mod / go.sum / go.work / go.work.sum before modifying them.
	// Cleanup() restores from this backup, so the backup must exist before any
	// modification happens — including when otelc setup is run standalone.
	backupFiles := []string{"go.mod", "go.sum", "go.work", "go.work.sum"}
	if err := util.BackupFile(backupFiles); err != nil {
		logger.DebugContext(ctx, "failed to back up files", "error", err)
	}

	sp := &SetupPhase{
		logger:     logger,
		ruleConfig: cmd.String("rules"),
	}

	// Introduce additional hook code by generating otelc.runtime.go
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
		return ex.Wrapf(err, "extracting embedded instrumentation pkg")
	}

	// Match the hook code with these dependencies
	matched, err := sp.matchDeps(ctx, deps)
	if err != nil {
		return ex.Wrapf(err, "matching dependencies to hook rules")
	}

	// Generate otelc.runtime.go for all packages
	moduleDirs := make(map[string]bool)
	for _, pkg := range pkgs {
		// file-based builds use synthetic "command-line-arguments" packages
		if pkg.Module == nil && pkg.PkgPath != commandLineArgumentsPackage {
			sp.Warn("skipping package without module", "package", pkg.PkgPath)
			continue
		}

		pkgDir := getPackageDir(pkg)
		if pkgDir == "" {
			sp.Warn("skipping package without Go files", "package", pkg.PkgPath)
			continue
		}

		var moduleDir string
		if pkg.Module != nil {
			moduleDir = pkg.Module.Dir
		} else {
			if moduleDir, err = pkgload.ResolveModuleDir(ctx, pkgDir); err != nil {
				return ex.Wrapf(err, "finding module dir for package %s", pkg.PkgPath)
			}
		}

		// Introduce additional hook code by generating otelc.runtime.go
		if err = sp.addDeps(matched, pkgDir); err != nil {
			return ex.Wrapf(err, "adding deps for package at %s", pkgDir)
		}
		moduleDirs[moduleDir] = true
	}

	// Sync new dependencies to go.mod or vendor/modules.txt
	for moduleDir := range moduleDirs {
		if err = sp.syncDeps(ctx, matched, moduleDir); err != nil {
			return ex.Wrapf(err, "syncing deps in module dir %s", moduleDir)
		}
	}

	// Write the matched hook to matched.txt for further instrument phase
	return sp.store(matched)
}

// setupGoCache creates a persistent GOCACHE in .otelc-build/gocache if one isn't already set.
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
			// Unrecognized -flag=value: skip it
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
	slices.Sort(enabledBoolFlags)

	// Combine: value flags first, then boolean flags
	return append(valueFlags, enabledBoolFlags...)
}

// stripCFlag removes a -C flag from either position Go itself accepts:
//   - before build/install: [-C, dir, build, ...]  -> [build, ...]
//   - immediately after:    [build, -C, dir, ...]  -> [build, ...]
//
// Used to clean args before they are forwarded to the underlying `go build`,
// after Setup has already consumed -C and called os.Chdir.
func stripCFlag(args []string) []string {
	if _, rest := consumeCFlagPositional(args); len(rest) != len(args) {
		args = rest
	}
	if len(args) > 1 {
		if _, rest := consumeCFlagPositional(args[1:]); len(rest) != len(args[1:]) {
			args = append([]string{args[0]}, rest...)
		}
	}
	return args
}

// BuildWithToolexec builds the project with the toolexec mode
func BuildWithToolexec(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	logger := util.LoggerFromContext(ctx)

	// Setup already consumed any -C flag and called os.Chdir; strip it from
	// the args we forward to the underlying `go build` so it doesn't end up
	// after build flags (go requires -C before build flags).
	args = stripCFlag(args)

	// Add -toolexec=otelc to the original build command and run it
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
	// Add the rest (already stripped of -C above)
	restArgs := args[1:]
	if _, fileTargets, err2 := splitBuildTargets(restArgs); err2 == nil && len(fileTargets) > 0 {
		// add otelc.runtime.go manually to command line for file targets
		dir := filepath.Dir(fileTargets[0])
		otelcRuntimePath := filepath.Join(dir, OtelcRuntimeFile)
		if util.PathExists(otelcRuntimePath) {
			restArgs = append(restArgs, otelcRuntimePath)
		}
	}
	newArgs = append(newArgs, restArgs...)
	logger.InfoContext(ctx, "Running go build with toolexec", "args", newArgs)

	// Tell the sub-process the working directory
	env := os.Environ()
	pwd := util.GetOtelcWorkDir()
	util.Assert(pwd != "", "invalid working directory")
	env = append(env, fmt.Sprintf("%s=%s", util.EnvOtelcWorkDir, pwd))

	// Extract and forward build flags that affect the build context
	// This ensures `go list` resolves archives matching the current build
	if buildFlags := extractBuildFlags(args); len(buildFlags) > 0 {
		encoded := util.EncodeBuildFlags(buildFlags)
		env = append(env, fmt.Sprintf("%s=%s", util.EnvOtelcBuildFlags, encoded))
		logger.DebugContext(ctx, "forwarding build flags", "flags", buildFlags)
	}

	// Use a fresh GOCACHE to prevent cache pollution when modifying core packages
	env, err = setupGoCache(ctx, env)
	if err != nil {
		return ex.Wrapf(err, "configuring go cache")
	}

	return util.RunCmdWithEnv(ctx, env, newArgs...)
}

func GoBuild(ctx context.Context, cmd *cli.Command) error {
	logger := util.LoggerFromContext(ctx)

	// Clean up import tracking files from previous builds at the start
	// to prevent stale data from affecting this build.
	instrument.CleanupImportTrackingFiles()

	if !cmd.Args().Present() {
		return ex.Newf("no command provided. Only 'go build' and 'go install' are supported")
	}

	// `go` accepts -C either before or after build/install:
	//   go -C ./dir build ...
	//   go build -C ./dir ...
	// Look past a leading -C when validating the command form. The actual
	// os.Chdir happens in Setup, where -C is consumed from args.
	first := cmd.Args().First()
	if first == "-C" || first == "--C" || strings.HasPrefix(first, "-C=") || strings.HasPrefix(first, "--C=") {
		_, rest := consumeCFlagPositional(cmd.Args().Slice())
		if len(rest) == 0 {
			return ex.Newf("no command provided after -C. Only 'go build' and 'go install' are supported")
		}
		first = rest[0]
	}

	if first != "build" && first != "install" {
		return ex.Newf("unsupported command: %s. Only 'go build' and 'go install' are supported", first)
	}

	defer func() {
		// Remove otelc.runtime.go from each instrumented package directory.
		pkgs, pkgErr := getBuildPackages(ctx, cmd.Args().Tail()) // pass args without build/install
		if pkgErr != nil {
			logger.DebugContext(ctx, "failed to get build packages", "error", pkgErr)
		}
		for _, pkg := range pkgs {
			path := filepath.Join(pkg.Dir, OtelcRuntimeFile)
			if removeErr := os.RemoveAll(path); removeErr != nil {
				logger.DebugContext(ctx, "failed to remove generated file from package",
					"file", path, "error", removeErr)
			}
		}

		// Restore backed-up go.mod/go.sum but keep .otelc-build/ for debugging.
		// Users can run `otelc cleanup` to remove it explicitly.
		if cleanErr := Cleanup(ctx, false); cleanErr != nil {
			logger.DebugContext(ctx, "cleanup failed", "error", cleanErr)
		}
	}()

	statsEnabled := os.Getenv(util.EnvOtelcStats) != ""

	setupStart := time.Now()
	err := Setup(ctx, cmd)
	if err != nil {
		return err
	}
	if statsEnabled {
		logger.InfoContext(ctx, "setup stats", "duration", time.Since(setupStart))
	}
	logger.InfoContext(ctx, "Setup completed successfully")

	buildStart := time.Now()
	err = BuildWithToolexec(ctx, cmd)
	if err != nil {
		return err
	}
	if statsEnabled {
		logger.InfoContext(ctx, "build stats", "duration", time.Since(buildStart))
	}
	logger.InfoContext(ctx, "Instrumentation completed successfully")
	return nil
}
