// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	gobuild "go/build"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/instrument"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
	"golang.org/x/tools/go/packages"
)

type setupPhase struct {
	logger        *slog.Logger
	ruleConfig    string
	buildPackages []*packages.Package
	buildFlags    []string
	// pgo is the build's effective -pgo value; see pgoValue.
	pgo             string
	rootModulePaths []string
	// resolvedNames maps an import path to a package name, built from
	// buildPackages' dependency graph.
	resolvedNames map[string]string

	// fileImportsCache caches discovered source imports for add_file rules during setup.
	fileImportsCache map[string][]string
}

func (sp *setupPhase) getBuildContext() *gobuild.Context {
	bctx := gobuild.Default
	bctx.BuildTags = append(
		slices.Clone(gobuild.Default.BuildTags),
		util.UserBuildTags(util.SplitGoflags(os.Getenv("GOFLAGS")), sp.buildFlags)...)
	return &bctx
}

func (sp *setupPhase) Info(msg string, args ...any)  { sp.logger.Info(msg, args...) }
func (sp *setupPhase) Error(msg string, args ...any) { sp.logger.Error(msg, args...) }
func (sp *setupPhase) Warn(msg string, args ...any)  { sp.logger.Warn(msg, args...) }
func (sp *setupPhase) Debug(msg string, args ...any) { sp.logger.Debug(msg, args...) }

func setupDebugDir(pkgPath string) string {
	if pkgPath == "" {
		pkgPath = "main"
	}
	return filepath.Join(util.GetBuildTemp("debug"), util.EscapePackagePath(pkgPath))
}

func cleanupDebugArtifacts() error {
	if !instrument.DiffDebugEnabled() {
		return nil
	}
	return instrument.CleanupDebugArtifacts()
}

// keepForDebug copies the file to the build temp directory for debugging.
// Error is tolerated as it's not critical.
func keepForDebug(ctx context.Context, srcPath, pkgPath string) {
	logger := util.LoggerFromContext(ctx)
	var targetDir string
	switch {
	case pkgPath != "":
		targetDir = setupDebugDir(pkgPath)
	case filepath.Clean(filepath.Dir(srcPath)) == filepath.Clean(util.GetOtelcWorkDir()):
		targetDir = setupDebugDir("main")
	default:
		targetDir = setupDebugDir(filepath.Base(filepath.Dir(srcPath)))
	}
	dstPath := filepath.Join(targetDir, filepath.Base(srcPath))
	if err := writeDebugCopy(srcPath, dstPath); err != nil {
		logger.WarnContext(ctx, "failed to record added file", "path", srcPath, "error", err)
	}
}

// writeDebugCopy copies srcPath to dstPath in the build temp directory. It
// does not write through a symbolic link or a hard link: dstPath and its
// parent directories below the build temp directory must not be symbolic
// links, and dstPath is replaced with a new file (see util.WriteFileAtomic).
// On Linux, a file in the build temp directory can be a hard link to a file
// in vendor/ (see linkNewFile), so a write through a link can change the
// vendored files of the user.
func writeDebugCopy(srcPath, dstPath string) error {
	root := util.GetBuildTempDir()
	if err := checkNoSymlink(root, dstPath); err != nil {
		return err
	}
	info, err := os.Stat(srcPath)
	if err != nil {
		return ex.Wrapf(err, "reading %s", srcPath)
	}
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return ex.Wrapf(err, "reading %s", srcPath)
	}
	if err = os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return ex.Wrapf(err, "creating the directory of %s", dstPath)
	}
	return util.WriteFileAtomic(dstPath, data, info.Mode().Perm())
}

// This function can be used to check if the setup has been completed.
func isSetup() bool {
	// TODO: Implement Task
	return false
}

// Go subcommands that otelc wraps with toolexec instrumentation.
const (
	subcmdBuild   = "build"
	subcmdInstall = "install"
	subcmdTest    = "test"

	goModFileName = "go.mod"
	vendorDirName = "vendor"
	flagMod       = "-mod"
)

// Go command flags that the argument scanners treat specially.
const (
	// flagArgs separates the go command's own arguments from the ones it
	// passes through to the test binary.
	flagArgs          = "-args"
	flagArgsDashDash  = "--args"
	delimiterDashDash = "--"
	// flagJSON makes the go command report its output as JSON events.
	flagJSON = "-json"
	// flagGcflags is forwarded to `go list` only with a package pattern; see
	// forwardedValueFlag.
	flagGcflags = "-gcflags"
)

func lastFileTargetIndex(subcommand string, args []string) int {
	last := -1
	for _, arg := range classifyArgs(subcommand, args) {
		if arg.Kind == ArgTarget && filepath.Ext(arg.Raw) == ".go" {
			last = arg.Index
		}
	}
	return last
}

// getBuildPackages loads all packages from the otelc go build/install/test or otelc setup command arguments.
// If no package patterns are found in args, defaults to loading the current directory package.
// subcommand is the go subcommand args from "build", "install", or "test".
// The args parameter should be the go build/install/test command arguments (e.g., ["-a", "./cmd"]).
// It returns two lists:
//   - the build packages: the real packages the go command builds. A test load
//     also returns test packages ("p [p.test]" variants, external "p_test"
//     packages, and the synthesized "p.test" binaries). They are excluded,
//     because otelc.runtime.go for one of them would declare "package p_test"
//     next to "p.go" and fail the build.
//   - all packages: the build packages plus those test packages. Only this list
//     sees them, because they carry imports that only _test.go files use.
//
// Returns an error if package loading fails or if invalid patterns are provided.
// For example:
//   - args ["-a", "./cmd"] returns packages for "./cmd"
//   - args ["-a", "cmd"] returns packages for the "cmd" package in the module
//   - args ["-a", ".", "./cmd"] returns packages for both "." and "./cmd"
//   - args [] returns packages for "."
//
//nolint:revive // needed to balance confusing-results and nonamedreturns linters
func getBuildPackages(
	ctx context.Context,
	subcommand string,
	args []string,
) ([]*packages.Package, []*packages.Package, error) {
	// NeedForTest marks the test packages, so loadBuildPackages can split them
	// from the real build packages.
	mode := packages.NeedName | packages.NeedFiles | packages.NeedModule |
		packages.NeedImports | packages.NeedDeps | packages.NeedForTest
	return loadBuildPackages(ctx, mode, subcommand, args)
}

// loadBuildPackages loads the build packages of getBuildPackages with the
// given load mode.
//
//nolint:revive // needed to balance confusing-results and nonamedreturns linters
func loadBuildPackages(
	ctx context.Context,
	mode packages.LoadMode,
	subcommand string,
	args []string,
) ([]*packages.Package, []*packages.Package, error) {
	logger := util.LoggerFromContext(ctx)
	tests := subcommand == subcmdTest

	pkgTargets, fileTargets, err := splitBuildTargets(subcommand, args)
	if err != nil {
		return nil, nil, err
	}
	buildFlags := extractBuildFlags(subcommand, args)

	var (
		pkgs    []*packages.Package
		loadErr error
	)
	switch {
	case len(fileTargets) > 0:
		pkgs, loadErr = pkgload.LoadPackages(ctx, mode, buildFlags, tests, fileTargets...)
		if loadErr != nil {
			return nil, nil, loadErr
		}

		if len(pkgs) > 1 {
			return nil, nil, ex.New("multiple packages found for file targets")
		}
	case len(pkgTargets) > 0:
		pkgs, loadErr = pkgload.LoadPackages(ctx, mode, buildFlags, tests, pkgTargets...)
		if loadErr != nil {
			return nil, nil, loadErr
		}
	default:
		pkgs, loadErr = pkgload.LoadPackages(ctx, mode, buildFlags, tests, ".")
		if loadErr != nil {
			return nil, nil, loadErr
		}
	}

	// A test load sets ForTest on every test package it adds. The synthesized
	// test binaries ("p.test") have no marker of their own, but each one is the
	// ForTest value of a test package plus a ".test" suffix.
	testBinaries := make(map[string]bool, len(pkgs))
	for _, pkg := range pkgs {
		if pkg.ForTest != "" {
			testBinaries[pkg.ForTest+".test"] = true
		}
	}

	buildPkgs := make([]*packages.Package, 0, len(pkgs))
	allPkgs := make([]*packages.Package, 0, len(pkgs))
	for _, pkg := range pkgs {
		// file-based builds use synthetic "command-line-arguments" packages
		if len(pkg.Errors) > 0 || (pkg.Module == nil && pkg.PkgPath != pkgload.CommandLineArgumentsPackage) {
			logger.DebugContext(ctx, "skipping package", "name", pkg.Name, "errors", pkg.Errors, "args", args)
			continue
		}

		allPkgs = append(allPkgs, pkg)
		if pkg.ForTest == "" && !testBinaries[pkg.ID] {
			buildPkgs = append(buildPkgs, pkg)
		}
	}

	if len(buildPkgs) == 0 {
		return nil, nil, ex.New("no valid packages found in build targets")
	}

	return buildPkgs, allPkgs, nil
}

//nolint:revive // if we add named returns then nonamedreturns will complain
func splitBuildTargets(subcommand string, args []string) ([]string, []string, error) {
	classified := classifyArgs(subcommand, args)
	var pkgs, files []string

	for i, a := range classified {
		if (a.Kind == ArgBuildFlag || a.Kind == ArgTestFlag) && !a.HasValue && takesValue(a.FlagName) {
			if i == len(classified)-1 ||
				(i+1 < len(classified) && classified[i+1].Kind != ArgBuildFlagValue && classified[i+1].Kind != ArgTestFlagValue) {
				return nil, nil, ex.Newf("flag %q requires a value", a.Raw)
			}
		}

		if a.Kind == ArgTarget {
			if filepath.Ext(a.Raw) == ".go" {
				files = append(files, a.Raw)
			} else {
				pkgs = append(pkgs, a.Raw)
			}
		}
	}

	if len(files) > 0 && len(pkgs) > 0 {
		return nil, nil, ex.New("cannot mix .go files and packages")
	}

	if len(files) > 0 {
		// All named .go files must live in one directory; compare each against
		// the first.
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

func rootModulePaths(ctx context.Context, pkgs []*packages.Package) ([]string, error) {
	roots := make(map[string]bool)
	for _, pkg := range pkgs {
		if pkg.Module != nil && pkg.Module.Path != "" {
			roots[pkg.Module.Path] = true
			continue
		}
		pkgDir := pkgload.PackageDir(pkg)
		if pkgDir == "" {
			continue
		}
		// otelc can have changed go.mod before this load, so the load uses
		// the -mod flag of the build.
		mod, err := pkgload.ResolveModule(ctx, pkgDir, modBuildFlags(ctx)...)
		if err != nil {
			return nil, err
		}
		if mod.Path != "" {
			roots[mod.Path] = true
		}
	}
	return slices.Sorted(maps.Keys(roots)), nil
}

// generateRuntimePerPackage generates the injected hook code (otelc.runtime.go)
// for every buildable package.
func (sp *setupPhase) generateRuntimePerPackage(
	ctx context.Context,
	pkgs []*packages.Package,
	matched []*rule.InstRuleSet,
) error {
	for _, pkg := range pkgs {
		pkgDir := pkgload.PackageDir(pkg)
		if pkgDir == "" {
			sp.Warn("skipping package without Go files", "package", pkg.PkgPath)
			continue
		}

		// Introduce additional hook code by generating otelc.runtime.go
		if err := sp.addDeps(ctx, matched, runtimePackage{
			dir:        pkgDir,
			importPath: sp.runtimeImportPath(ctx, pkg, pkgDir),
			debugPath:  pkg.PkgPath,
			name:       pkg.Name,
		}); err != nil {
			return ex.Wrapf(err, "adding deps for package at %s", pkgDir)
		}
	}

	return nil
}

// runtimeImportPath returns the import path of the package that receives a
// generated runtime file. A file target loads one synthetic
// "command-line-arguments" package, so the real path comes from the module that
// owns the directory. If the lookup fails, runtimeImportPath returns the
// synthetic path, and a build outside a module continues to work.
func (sp *setupPhase) runtimeImportPath(ctx context.Context, pkg *packages.Package, pkgDir string) string {
	if pkg.PkgPath != pkgload.CommandLineArgumentsPackage {
		return pkg.PkgPath
	}

	importPath, err := resolveImportPath(ctx, sp.buildFlags, pkgDir)
	if err != nil {
		// Expected outside a module, so this is not a warning on its own.
		sp.Debug("cannot derive the import path of a file target", "dir", pkgDir, "error", err)
		return pkg.PkgPath
	}

	return importPath
}

// resolveImportPath returns the canonical import path of the package in pkgDir.
// resolveImportPath loads pkgDir as a package pattern, so the result is the real
// import path and not the synthetic path that a file target carries.
//
// The build flags are the ones of the build, unchanged. Build tags can exclude
// every file in pkgDir, and relative -modfile or -overlay values resolve against
// the -C directory of the build, so pkgDir cannot replace that directory.
func resolveImportPath(ctx context.Context, buildFlags []string, pkgDir string) (string, error) {
	pkgs, err := pkgload.LoadPackages(ctx, packages.NeedName, buildFlags, false, pkgDir)
	if err != nil {
		return "", err
	}
	if len(pkgs) == 0 || len(pkgs[0].Errors) > 0 || pkgs[0].PkgPath == "" {
		return "", ex.Newf("cannot resolve the import path of the package in %s", pkgDir)
	}

	return pkgs[0].PkgPath, nil
}

// Setup prepares the environment for further instrumentation. It runs
// under the build lock; when invoked from GoBuild the surrounding lock is
// reused via the context marker instead of re-acquired.
func Setup(ctx context.Context, cmd *cli.Command) error {
	return withBuildLock(ctx, func(ctx context.Context) error {
		return setupLocked(ctx, cmd)
	})
}

func setupLocked(ctx context.Context, cmd *cli.Command) error {
	// Since Setup can be invoked in different contexts (i.e, via `otelc setup` or as part of `otelc go build`),
	// we need to handle the arguments accordingly. If the command is `go build` or `go install`, we should trim the first argument
	args := cmd.Args().Slice()
	subcommand := subcmdBuild
	if cmd.Name == "go" {
		subcommand = cmd.Args().First() // build / install / test
		args = cmd.Args().Tail()        // trim the subcommand
	} else if err := cleanupDebugArtifacts(); err != nil {
		// Clean up debug artifacts from previous runs for standalone `otelc setup`,
		// as it prepares the environment for a subsequent toolexec build.
		// For `otelc go ...`, runGoBuild already performed this cleanup at its
		// lifecycle boundary before invoking Setup.
		return ex.Wrapf(err, "cleaning debug artifacts")
	}

	logger := util.LoggerFromContext(ctx)

	// A standalone `otelc setup` (GoBuild puts a state manager in ctx and
	// recovers first): restore the changes that a previous build or setup
	// left, before the files are read and tracked again.
	if _, found := stateManagerFromContext(ctx); !found {
		if err := recoverLeftoverState(ctx); err != nil {
			return err
		}
	}

	// A vendored project keeps vendor mode: the dependencies of the user come
	// only from vendor/. The modules of the hook code are added to vendor/
	// for the time of the build (see vendorSync). The pin and sync steps
	// below read the mode from the context.
	ctx, err := contextWithBuildMode(ctx, subcommand, args)
	if err != nil {
		return err
	}

	if isSetup() {
		logger.InfoContext(ctx, "Setup has already been completed, skipping setup.")
		return nil
	}

	sp := &setupPhase{
		logger:     logger,
		ruleConfig: cmd.String("rules"),
		buildFlags: extractBuildFlags(subcommand, args),
		pgo:        pgoValue(subcommand, args),
	}

	// Introduce additional hook code by generating otelc.runtime.go
	// Use GetPackage to determine the build target directory
	buildPkgs, allPkgs, err := getBuildPackages(ctx, subcommand, args)
	if err != nil {
		return err
	}
	// Only the real packages are built. The test packages still feed the name
	// table, because they carry imports that only _test.go files use.
	sp.buildPackages = buildPkgs
	sp.resolvedNames = pkgload.CollectPackageNames(allPkgs)

	// Find the module directories for the build packages
	moduleDirs, findModErr := pkgload.FindModuleDirs(ctx, buildPkgs)
	if findModErr != nil {
		return findModErr
	}

	// Ensure a state manager is available in the context
	if stateManager, found := stateManagerFromContext(ctx); !found {
		// save this state manager in the context
		ctx = contextWithStateManager(ctx, stateManager)
	}

	deps, err := sp.buildDeps(ctx, moduleDirs, subcommand, args)
	if err != nil {
		return err
	}

	// Match the hook code with these dependencies
	matched, err := sp.matchDeps(ctx, deps, moduleDirs)
	if err != nil {
		return ex.Wrapf(err, "matching dependencies to hook rules")
	}

	// Resolve rule import paths before matching injected dependencies so add_file
	// sources can be located and parsed for dependencies that must enter the build graph.
	if err = sp.resolveRuleSetPaths(ctx, matched, moduleDirs); err != nil {
		return ex.Wrapf(err, "resolving rule paths")
	}

	// The hook packages the pass above selected get blank-imported into the
	// application below, which compiles their dependencies too. The build plan
	// never saw those, so match them now.
	injected, err := sp.matchInjectedDeps(
		ctx, matched, deps, moduleDirs, sp.buildFlags,
	)
	if err != nil {
		return err
	}
	matched = append(matched, injected...)

	// Reported here rather than per matching pass: an individual pass matching
	// nothing is normal, it is the combined result that tells the user whether
	// anything will be instrumented.
	if len(matched) == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: no instrumentation will be applied\n")
		sp.Warn("no instrumentation rules matched any dependencies")
	}

	// Generate otelc.runtime.go for all packages
	if err = sp.generateRuntimePerPackage(ctx, buildPkgs, matched); err != nil {
		return err
	}

	// Write the matched ruleset to matched.json for further instrument phase
	return sp.store(matched)
}

// buildDeps returns the dependencies of the build. Without custom rules, it
// runs autoPin, which generates or updates the otel.instrumentation.go file.
func (sp *setupPhase) buildDeps(
	ctx context.Context,
	moduleDirs []string,
	subcommand string,
	args []string,
) ([]*Dependency, error) {
	if !sp.usesCustomRules() {
		pinResult, err := autoPin(ctx, moduleDirs, subcommand, args)
		if err != nil {
			return nil, err
		}
		if pinResult.AllDeps != nil {
			return pinResult.AllDeps, nil
		}
	}
	// Find all dependencies of the project being build
	return findDeps(ctx, subcommand, args)
}

// usesCustomRules reports whether the build uses custom rules (--rules or
// OTELC_RULES) in place of the rules of the tool files.
func (sp *setupPhase) usesCustomRules() bool {
	return sp.ruleConfig != "" || os.Getenv(util.EnvOtelcRules) != ""
}

// resolveRuleSetPaths resolves the paths of the rules in matched (see
// resolveRulePaths), and adds the packages that these rules bring into the
// build to vendor/ when this is necessary (see syncHooks).
//
// resolveRulePaths loads the hook packages, so they must be in vendor/ before
// it runs. The imports of the add_file source files are known only after the
// paths are resolved (see fileRuleSourceImports), and the next injection pass
// loads them: thus a second sync adds them to vendor/.
func (sp *setupPhase) resolveRuleSetPaths(
	ctx context.Context,
	matched []*rule.InstRuleSet,
	moduleDirs []string,
) error {
	if err := sp.syncHooks(ctx, matched, moduleDirs); err != nil {
		return err
	}
	if err := resolveRulePaths(ctx, matched, moduleDirs); err != nil {
		return err
	}
	if !slices.ContainsFunc(matched, func(rs *rule.InstRuleSet) bool { return len(rs.FileRules) > 0 }) {
		return nil
	}
	return sp.syncHooks(ctx, matched, moduleDirs)
}

// syncHooks adds the packages that the rules of matched bring into the build
// to vendor/ (see vendorSyncRuleSets), when this is necessary.
//
// Custom rules skip autoPin, which adds the hook packages to vendor/ in vendor
// mode. So, for custom rules in vendor mode, otelc adds the packages of the
// rules to vendor/ before it loads them. A rule that matches in one injection
// pass can add a new hook package, and the next pass loads this hook package:
// thus the sync must occur for the rule sets of each pass, not only one time
// at the start.
func (sp *setupPhase) syncHooks(ctx context.Context, matched []*rule.InstRuleSet, moduleDirs []string) error {
	if vendored, _ := vendorModeFromContext(ctx); !vendored || !sp.usesCustomRules() {
		return nil
	}
	return sp.vendorSyncRuleSets(ctx, matched, moduleDirs)
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

// extractBuildFlags extracts flags that affect the build context from the arguments.
// These flags need to be forwarded to `go list` when resolving import archives.
// Returns a slice of flag arguments preserving their original form.
// Only patterned -gcflags values are forwarded: unpatterned values apply to the
// outer command's targets, not the imports resolved by the nested go command.
//
// For boolean flags, the last occurrence wins. This correctly handles cases like:
//   - GOFLAGS=-race with -race=false on CLI (result: -race=false)
//   - -race -race=false (result: -race=false)
//   - -race=false -race (result: -race)
func extractBuildFlags(subcommand string, args []string) []string {
	if subcommand == "" {
		subcommand = subcmdBuild
	}
	classified := classifyArgs(subcommand, args)
	var valueFlags []string
	type boolFlagValue struct {
		set   bool
		value bool
	}
	boolFlagState := make(map[string]boolFlagValue) // Track final state of boolean flags

	for i, a := range classified {
		if a.Kind != ArgBuildFlag {
			continue
		}

		if isBuildContextFlagWithValue(a.FlagName) {
			valueFlags = append(valueFlags, forwardedValueFlag(classified, i)...)
			continue
		}

		if isBuildContextBoolFlag(a.FlagName) {
			if a.HasValue {
				if enabled, err := strconv.ParseBool(a.Value); err == nil {
					boolFlagState[a.FlagName] = boolFlagValue{set: true, value: enabled} // Last value wins
				}
			} else {
				boolFlagState[a.FlagName] = boolFlagValue{set: true, value: true}
			}
			continue
		}
	}

	// Collect boolean flags that are enabled (in deterministic order)
	var enabledBoolFlags []string
	for flag, state := range boolFlagState {
		if state.set {
			if state.value {
				enabledBoolFlags = append(enabledBoolFlags, flag)
			} else {
				enabledBoolFlags = append(enabledBoolFlags, flag+"=false")
			}
		}
	}
	slices.Sort(enabledBoolFlags)

	return append(valueFlags, enabledBoolFlags...)
}

// forwardedValueFlag returns the tokens of the value flag at classified[i] as
// the user wrote them, in its joined or separated form, or nil when it has no
// value or must not be forwarded. cmd/go applies a -gcflags value without a
// package pattern only to the packages named on its own command line: the
// targets of this build, but the added import in a nested `go list`.
func forwardedValueFlag(classified []ClassifiedArg, i int) []string {
	a := classified[i]
	tokens := []string{a.Raw}
	value := a.Value
	if !a.HasValue {
		if i+1 >= len(classified) || classified[i+1].Kind != ArgBuildFlagValue {
			return nil
		}
		value = classified[i+1].Raw
		tokens = append(tokens, value)
	}
	if a.FlagName == flagGcflags && !util.HasPackagePattern(value) {
		return nil
	}
	return tokens
}

// toolexecInsertArg builds the -toolexec=... argument for execPath. Split out
// from buildWithToolexec so the error path (reachable only when execPath
// contains both quote characters) is testable without depending on
// os.Executable.
func toolexecInsertArg(execPath string) (string, error) {
	insert, err := util.BuildToolexecFlag(execPath)
	if err != nil {
		// BuildToolexecFlag's own error already names execPath, so there
		// is nothing to add here, matching nestedToolexecGoflagsToken
		// and the #1231 convention.
		return "", err
	}
	return insert, nil
}

// addBuildFlags inserts flags after a leading -C, which the go command requires
// to be the first flag on its command line.
func addBuildFlags(args []string, flags ...string) []string {
	insertAt := 0
	if len(args) > 0 {
		switch {
		case (args[0] == "-C" || args[0] == "--C") && len(args) > 1:
			insertAt = 2
		case strings.HasPrefix(args[0], "-C=") || strings.HasPrefix(args[0], "--C="):
			insertAt = 1
		}
	}

	result := make([]string, 0, len(args)+len(flags))
	result = append(result, args[:insertAt]...)
	result = append(result, flags...)
	return append(result, args[insertAt:]...)
}

// toolexecBuildArgs assembles the argv for the instrumented build: the
// original go subcommand, optional goFlags, the -toolexec flag pointing at execPath, and
// the caller's remaining arguments. Kept free of side effects so it can be
// tested directly; buildWithToolexec cannot be, since it would spawn a go
// build whose -toolexec target is the test binary itself.
func toolexecBuildArgs(args []string, execPath string, goFlags ...string) ([]string, error) {
	insert, err := toolexecInsertArg(execPath)
	if err != nil {
		return nil, err
	}
	const additionalCount = 2
	newArgs := make([]string, 0, len(args)+additionalCount+len(goFlags)) // Avoid in-place modification
	// Add "go build"
	newArgs = append(newArgs, "go")
	newArgs = append(newArgs, args[:1]...)
	// Add the rest
	subcommand := args[0]
	restArgs := args[1:]
	if _, fileTargets, err2 := splitBuildTargets(subcommand, restArgs); err2 == nil && len(fileTargets) > 0 {
		// add otelc.runtime.go manually to command line for file targets
		dir := filepath.Dir(fileTargets[0])
		otelcRuntimePath := filepath.Join(dir, otelcRuntimeFile)
		if util.PathExists(otelcRuntimePath) {
			// Keep the runtime file next to the file targets. In go test,
			// a file path after a flag is passed to the test binary instead.
			insertAt := lastFileTargetIndex(subcommand, restArgs) + 1
			restArgs = slices.Insert(restArgs, insertAt, otelcRuntimePath)
		}
	}
	// Add goFlags and -toolexec after a leading -C/--C. The go command rejects -C
	// when any other flag comes before it.
	restArgs = addBuildFlags(restArgs, slices.Concat(goFlags, []string{insert})...)
	return append(newArgs, restArgs...), nil
}

// runBuildCmd runs the assembled go build. It is a variable so tests can
// replace it: the -toolexec target is os.Executable(), which under `go test`
// is the test binary, so really running the command would make the test
// binary its own toolexec target and re-invoke itself without bound.
//
//nolint:gochecknoglobals // test seam
var runBuildCmd = util.RunCmdWithEnv

// executablePath resolves the path to the current executable. It is a variable
// so tests can simulate paths that trigger quoting errors.
//
//nolint:gochecknoglobals // test seam
var executablePath = os.Executable

// buildWithToolexec builds the project with the toolexec mode. The -mod flag of
// the user is kept: a vendored project builds in vendor mode, with the modules
// of the hook code that setup added to vendor/.
func buildWithToolexec(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	logger := util.LoggerFromContext(ctx)

	// Add -toolexec=otelc to the original build command and run it
	execPath, err := executablePath()
	if err != nil {
		return ex.Wrapf(err, "failed to get executable path")
	}
	var goFlags []string
	if cmd.Bool("debug") {
		// Keep Go's temporary work directory only for debugging.
		goFlags = append(goFlags, "-work")
	}
	newArgs, err := toolexecBuildArgs(args, execPath, goFlags...)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "Running go build with toolexec", "args", newArgs)

	// Tell the sub-process the working directory
	env := os.Environ()
	pwd := util.GetOtelcWorkDir()
	util.Assert(pwd != "", "invalid working directory")
	env = append(env, fmt.Sprintf("%s=%s", util.EnvOtelcWorkDir, pwd))

	// Extract and forward build flags that affect the build context
	// This ensures `go list` resolves archives matching the current build
	subcommand := args[0]
	restArgs := args[1:]
	buildFlags := extractBuildFlags(subcommand, restArgs)
	if len(buildFlags) > 0 {
		encoded := util.EncodeBuildFlags(buildFlags)
		env = append(env, fmt.Sprintf("%s=%s", util.EnvOtelcBuildFlags, encoded))
		logger.DebugContext(ctx, "forwarding build flags", "flags", buildFlags)
	}

	// The toolexec processes load packages too (for example to resolve the
	// imports that rules add). The go command does not give them the -mod
	// flag of the command line. So the -mod flag goes in GOFLAGS: else, for
	// example, the loads of a -mod=mod build of a vendored module use vendor
	// mode, and fail with "inconsistent vendoring".
	env = envWithModFlag(env, lastModFlag(subcommand, restArgs))

	// Use a fresh GOCACHE to prevent cache pollution when modifying core packages
	env, err = setupGoCache(ctx, env)
	if err != nil {
		return ex.Wrapf(err, "configuring go cache")
	}

	return runBuildCmd(ctx, env, newArgs...)
}

func GoBuild(ctx context.Context, cmd *cli.Command) error {
	// Validate the invocation before taking the build lock: a bad command
	// line must fail immediately, not wait behind a running build.
	if !cmd.Args().Present() {
		return ex.Newf("no command provided. Only 'go build', 'go install' and 'go test' are supported")
	}

	switch cmd.Args().First() {
	case subcmdBuild, subcmdInstall, subcmdTest:
		// supported
	default:
		return ex.Newf("unsupported command: %s. Only 'go build', 'go install' and 'go test' are supported",
			cmd.Args().First())
	}

	// Serialize with other otelc invocations in this module before touching
	// any shared state (tracking files, go.mod, .otelc-build contents).
	return withBuildLock(ctx, func(ctx context.Context) error {
		return runGoBuild(ctx, cmd)
	})
}

func runGoBuild(ctx context.Context, cmd *cli.Command) (err error) {
	// A build that stopped before its cleanup left changed files. Restore
	// them before this build tracks the files again.
	if err = recoverLeftoverState(ctx); err != nil {
		return err
	}
	ctx = contextWithStateManager(ctx, newStateManager())
	logger := util.LoggerFromContext(ctx)

	// Clean up import tracking files from previous builds at the start
	// to prevent stale data from affecting this build.
	instrument.CleanupImportTrackingFiles()
	if err = cleanupDebugArtifacts(); err != nil {
		return ex.Wrapf(err, "cleaning debug artifacts")
	}

	defer func() {
		// Restore backed-up go.mod/go.sum but keep .otelc-build/ for debugging.
		// Users can run `otelc cleanup` to remove it explicitly. Deferred
		// inside the locked body so the restore always runs under the lock.
		// A failed restore fails the build: the files of the user are not
		// the same as before.
		if cleanErr := Cleanup(ctx, false); cleanErr != nil {
			err = ex.Join(err, cleanErr)
		}
	}()

	statsEnabled := os.Getenv(util.EnvOtelcStats) != ""

	setupStart := time.Now()
	err = Setup(ctx, cmd)
	if err != nil {
		return err
	}
	if statsEnabled {
		logger.InfoContext(ctx, "setup stats", "duration", time.Since(setupStart))
	}
	logger.InfoContext(ctx, "Setup completed successfully")

	buildStart := time.Now()
	err = buildWithToolexec(ctx, cmd)
	if err != nil {
		return err
	}
	if statsEnabled {
		logger.InfoContext(ctx, "build stats", "duration", time.Since(buildStart))
	}
	logger.InfoContext(ctx, "Instrumentation completed successfully")
	return nil
}
