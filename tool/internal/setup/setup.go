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

type buildDirPlacement int

const (
	buildDirPlacementNone buildDirPlacement = iota
	buildDirPlacementBeforeSubcommand
	buildDirPlacementAfterSubcommand
)

type buildInvocation struct {
	command          string
	buildDir         string
	buildDirPosition buildDirPlacement
	args             []string
}

func (inv buildInvocation) appendGoCommand(args []string) []string {
	args = append(args, "go")
	if inv.buildDirPosition == buildDirPlacementBeforeSubcommand && inv.buildDir != "" {
		args = append(args, "-C", inv.buildDir)
	}
	args = append(args, inv.command)
	if inv.buildDirPosition == buildDirPlacementAfterSubcommand && inv.buildDir != "" {
		args = append(args, "-C", inv.buildDir)
	}
	return args
}

// minArgsForCFlag is the minimum number of arguments required when the first
// argument is "-C": the flag itself, the directory, and the subcommand.
const minArgsForCFlag = 3

func parseGoInvocation(args []string) (buildInvocation, error) {
	if len(args) == 0 {
		return buildInvocation{}, ex.Newf("no command provided. Only 'go build' and 'go install' are supported")
	}

	invocation := buildInvocation{buildDirPosition: buildDirPlacementNone}
	switch args[0] {
	case "build", "install":
		invocation.command = args[0]
		invocation.args = args[1:]
		if len(invocation.args) >= 2 && invocation.args[0] == "-C" {
			invocation.buildDir = invocation.args[1]
			invocation.buildDirPosition = buildDirPlacementAfterSubcommand
			invocation.args = invocation.args[2:]
		}
		return invocation, nil
	case "-C":
		if len(args) < minArgsForCFlag {
			return buildInvocation{}, ex.Newf("no command provided. Only 'go build' and 'go install' are supported")
		}
		if args[2] != "build" && args[2] != "install" {
			return buildInvocation{}, ex.Newf(
				"unsupported command: %s. Only 'go build' and 'go install' are supported",
				args[2],
			)
		}
		invocation.command = args[2]
		invocation.buildDir = args[1]
		invocation.buildDirPosition = buildDirPlacementBeforeSubcommand
		invocation.args = args[3:]
		return invocation, nil
	default:
		return buildInvocation{}, ex.Newf(
			"unsupported command: %s. Only 'go build' and 'go install' are supported",
			args[0],
		)
	}
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

func absBuildPath(buildDir, target string) (string, error) {
	if filepath.IsAbs(target) {
		return filepath.Clean(target), nil
	}
	if buildDir != "" {
		target = filepath.Join(buildDir, target)
	}
	return filepath.Abs(target)
}

func resolveDirRelativeToBuild(buildDir, dir string) string {
	if buildDir == "" || filepath.IsAbs(dir) || strings.HasPrefix(dir, "$") {
		return dir
	}
	absDir, err := absBuildPath(buildDir, dir)
	if err != nil {
		return dir
	}
	return absDir
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
func getBuildPackages(ctx context.Context, buildDir string, args []string) ([]*packages.Package, error) {
	logger := util.LoggerFromContext(ctx)
	mode := packages.NeedName | packages.NeedFiles | packages.NeedModule

	pkgTargets, fileTargets, err := splitBuildTargets(buildDir, args)
	if err != nil {
		return nil, ex.Wrapf(err, "splitting build targets")
	}

	var (
		pkgs    []*packages.Package
		loadErr error
	)
	switch {
	case len(fileTargets) > 0:
		pkgs, loadErr = pkgload.LoadPackages(ctx, mode, nil, buildDir, fileTargets...)
		if loadErr != nil {
			return nil, ex.Wrapf(loadErr, "failed to load packages for files %v", fileTargets)
		}

		if len(pkgs) > 1 {
			return nil, ex.New("multiple packages found for file targets")
		}
	case len(pkgTargets) > 0:
		pkgs, loadErr = pkgload.LoadPackages(ctx, mode, nil, buildDir, pkgTargets...)
		if loadErr != nil {
			return nil, ex.Wrapf(loadErr, "failed to load packages for patterns %v", pkgTargets)
		}
	default:
		pkgs, loadErr = pkgload.LoadPackages(ctx, mode, nil, buildDir, ".")
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
func splitBuildTargets(buildDir string, args []string) ([]string, []string, error) {
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
		dir, err := absBuildPath(buildDir, filepath.Dir(files[0]))
		if err != nil {
			return nil, nil, ex.Wrapf(err, "failed to get absolute path for directory containing files")
		}

		for _, f := range files[1:] {
			fdir, err2 := absBuildPath(buildDir, filepath.Dir(f))
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
func Setup(ctx context.Context, cmd *cli.Command, invocation buildInvocation) error {
	logger := util.LoggerFromContext(ctx)

	if isSetup() {
		logger.InfoContext(ctx, "Setup has already been completed, skipping setup.")
		return nil
	}

	sp := &SetupPhase{
		logger:     logger,
		ruleConfig: cmd.String("rules"),
	}

	// Introduce additional hook code by generating otelc.runtime.go
	// Use GetPackage to determine the build target directory
	pkgs, err := getBuildPackages(ctx, invocation.buildDir, invocation.args)
	if err != nil {
		return err
	}

	// Find all dependencies of the project being build
	deps, err := sp.findDeps(ctx, invocation)
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

	// Backup go.mod, go.sum and go.work.sum files before modifying them
	if err = backupFiles(ctx, moduleDirs); err != nil {
		return ex.Wrapf(err, "backing up files")
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

// SetupCommand is the CLI entrypoint for `otelc setup`.
func SetupCommand(ctx context.Context, cmd *cli.Command) error {
	invocation, err := parseGoInvocation(cmd.Args().Slice())
	if err != nil {
		return err
	}
	return Setup(ctx, cmd, invocation)
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

// BuildWithToolexec builds the project with the toolexec mode
func BuildWithToolexec(ctx context.Context, _ *cli.Command, invocation buildInvocation) error {
	logger := util.LoggerFromContext(ctx)

	// Add -toolexec=otelc to the original build command and run it
	execPath, err := os.Executable()
	if err != nil {
		return ex.Wrapf(err, "failed to get executable path")
	}
	insert := "-toolexec=" + execPath + " toolexec"
	newArgs := invocation.appendGoCommand(nil)
	// Add "-work" to give us a chance to debug instrumented code if needed
	newArgs = append(newArgs, "-work")
	// Add "-toolexec=..."
	newArgs = append(newArgs, insert)
	// Add the rest
	restArgs := invocation.args
	if _, fileTargets, err2 := splitBuildTargets(invocation.buildDir, restArgs); err2 == nil && len(fileTargets) > 0 {
		// add otelc.runtime.go manually to command line for file targets
		dir, dirErr := absBuildPath(invocation.buildDir, filepath.Dir(fileTargets[0]))
		if dirErr != nil {
			return ex.Wrapf(dirErr, "resolving runtime file directory")
		}
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
	if buildFlags := extractBuildFlags(invocation.args); len(buildFlags) > 0 {
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

	invocation, err := parseGoInvocation(cmd.Args().Slice())
	if err != nil {
		return err
	}

	defer func() {
		// Restore backed-up go.mod/go.sum but keep .otelc-build/ for debugging.
		// Users can run `otelc cleanup` to remove it explicitly.
		if cleanErr := Cleanup(ctx, invocation.buildDir, invocation.args, false); cleanErr != nil {
			logger.DebugContext(ctx, "cleanup failed", "error", cleanErr)
		}
	}()

	statsEnabled := os.Getenv(util.EnvOtelcStats) != ""

	setupStart := time.Now()
	err = Setup(ctx, cmd, invocation)
	if err != nil {
		return err
	}
	if statsEnabled {
		logger.InfoContext(ctx, "setup stats", "duration", time.Since(setupStart))
	}
	logger.InfoContext(ctx, "Setup completed successfully")

	buildStart := time.Now()
	err = BuildWithToolexec(ctx, cmd, invocation)
	if err != nil {
		return err
	}
	if statsEnabled {
		logger.InfoContext(ctx, "build stats", "duration", time.Since(buildStart))
	}
	logger.InfoContext(ctx, "Instrumentation completed successfully")
	return nil
}
