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
	"sync"
	"time"

	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/configuration"
	"go.opentelemetry.io/otelc/tool/internal/instrument"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
	"golang.org/x/tools/go/packages"
)

type setupPhase struct {
	decisionMu    *sync.Mutex
	decision      func(source string, r rule.InstRule, reason string)
	logger        *slog.Logger
	ruleConfig    string
	buildPackages []*packages.Package
	buildFlags    []string
	// runtimeImports holds, by directory, the imports rules add to
	// file-target packages (see prepareImportOverlay).
	runtimeImports  map[string][]string
	rootModulePaths []string
	// resolvedNames maps an import path to a package name, built from
	// buildPackages' dependency graph.
	resolvedNames map[string]string

	// fileImportsCache caches discovered source imports for add_file rules during setup.
	fileImportsCache map[string][]string
	testMode         bool
	// testPackages holds the test variants and test binaries of a go test
	// build, which buildPackages leaves out.
	testPackages []*packages.Package
	// configurationMetadata is the setup-time handoff to application config
	// validation, including selected modules that have no metadata file.
	configurationMetadata selectedConfigurationMetadata
	// ruleModuleRoots keeps instrumentation rule ownership outside the rules
	// themselves so setup can resolve module-local metadata for matched rules.
	ruleModuleRoots map[rule.InstRule]string
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
	if err := util.CopyFile(srcPath, dstPath); err != nil {
		logger.WarnContext(ctx, "failed to record added file", "path", srcPath, "error", err)
	}
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
	// flagCover and flagCoverpkg select the packages a build collects
	// coverage for; see nestedCoverFlags.
	flagCover    = "-cover"
	flagCoverpkg = "-coverpkg"
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
//     next to "p.go" and fail the build. Outside a test load, packages with
//     only _test.go files are excluded too, because the go command does not
//     build them.
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
		if pkg.ForTest != "" || testBinaries[pkg.ID] {
			continue
		}
		// A package with only _test.go files has no GoFiles. Only go test builds
		// it. Outside a test load, the go command skips it or reports the error.
		if !tests && len(pkg.GoFiles) == 0 {
			logger.DebugContext(ctx, "skipping package with only _test.go files", "package", pkg.PkgPath)
			continue
		}
		buildPkgs = append(buildPkgs, pkg)
	}

	if len(allPkgs) == 0 {
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
		mod, err := pkgload.ResolveModule(ctx, pkgDir)
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
	// What each hook package imports. A package a hook imports must not
	// import that hook back, or the build has an import cycle.
	imports, err := sp.hookImports(ctx, matched)
	if err != nil {
		return err
	}
	scope := sp.newRuntimeScope(ctx, pkgs, matched, imports)

	for _, pkg := range pkgs {
		pkgDir := pkgload.PackageDir(pkg)
		if pkgDir == "" {
			sp.Warn("skipping package without Go files", "package", pkg.PkgPath)
			continue
		}

		importPath := sp.runtimeImportPath(ctx, pkg, pkgDir)
		importedByHooks := make(map[string]bool)
		for hookPath, deps := range imports {
			if deps[importPath] {
				importedByHooks[hookPath] = true
			}
		}

		pkgMatched := runtimeRuleSets{pkg: matched}
		if scope != nil {
			pkgMatched = scope.ruleSets(pkg, importPath)
		}

		// Introduce additional hook code by generating otelc.runtime.go
		if err = sp.addDeps(ctx, pkgMatched.pkg, runtimePackage{
			dir:             pkgDir,
			importPath:      importPath,
			debugPath:       pkg.PkgPath,
			name:            pkg.Name,
			importedByHooks: importedByHooks,
			testRuleSets:    pkgMatched.test,
			testMode:        sp.testMode,
			fileTarget:      pkg.PkgPath == pkgload.CommandLineArgumentsPackage,
			addedImports:    sp.runtimeImports[pkgDir],
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
	return withBuildLock(ctx, commandBuildDir(cmd), func(ctx context.Context) error {
		return setupLocked(ctx, cmd)
	})
}

func (sp *setupPhase) matchAllDeps(
	ctx context.Context,
	deps []*Dependency,
	moduleDirs []string,
	buildFlags []string,
) ([]*rule.InstRuleSet, error) {
	// Prepare rules once for both the initial and injected dependencies.
	idx, err := sp.loadRuleIndex(ctx, moduleDirs)
	if err != nil {
		return nil, ex.Wrapf(err, "loading hook rules")
	}

	matched, err := sp.matchDeps(ctx, deps, idx)
	if err != nil {
		return nil, ex.Wrapf(err, "matching dependencies to hook rules")
	}

	// Resolve rule import paths before matching injected dependencies so add_file
	// sources can be located and parsed for dependencies that must enter the build graph.
	if err = resolveRulePaths(ctx, matched, moduleDirs); err != nil {
		return nil, ex.Wrapf(err, "resolving rule paths")
	}

	// The hook packages the pass above selected get blank-imported into the
	// application below, which compiles their dependencies too. The build plan
	// never saw those, so match them now.
	injected, err := sp.matchInjectedDeps(ctx, matched, deps, idx, buildFlags)
	if err != nil {
		return nil, err
	}
	// matchDeps already gives every batch a stable order, and the batches
	// are concatenated in a fixed sequence, so the combined slice is
	// deterministic without re-sorting it here. Re-sorting would re-encode
	// every set that shares a ModulePath a second time.
	matched = append(matched, injected...)

	runtimeSets, err := matchRuntimeGLS(deps)
	if err != nil {
		return nil, err
	}
	matched = append(matched, runtimeSets...)
	return matched, nil
}

func (sp *setupPhase) recordRuleModuleRoot(instRule rule.InstRule, moduleRoot string) {
	if sp.ruleModuleRoots == nil {
		sp.ruleModuleRoots = make(map[rule.InstRule]string)
	}
	sp.ruleModuleRoots[instRule] = moduleRoot
}

func (sp *setupPhase) validateAndStoreSelectedConfigurationMetadata(matched []*rule.InstRuleSet) error {
	metadata, err := sp.validateSelectedConfigurationMetadata(matched, configuration.CatalogDefinitions)
	if err != nil {
		return ex.Wrapf(err, "validating selected instrumentation configuration metadata")
	}
	sp.configurationMetadata = metadata
	return nil
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

	// Vendored projects fail the vendor consistency check: setup edits go.mod
	// for the injected hook modules but not vendor/modules.txt. Force module
	// mode so both build phases resolve dependency sources from the module cache
	// (matching versions and paths) instead of vendor/, leaving the user's
	// vendor directory untouched. This must run before isSetup() and
	// getBuildPackages() below so a cached-setup `otelc go build` still sets
	// GOFLAGS for the later buildWithToolexec, and before the findDeps dry run
	// further down. Computed here rather than threaded in because Setup is also
	// a standalone command action (otelc setup).
	if vendoringActive(ctx, util.GetOtelcWorkDir()) {
		logger.InfoContext(ctx, "vendored project detected; building with -mod=mod")
		// Mutates GOFLAGS process-wide with no restore. Fine for the one-shot
		// CLI; a second in-process GoBuild would inherit this -mod=mod.
		if err := os.Setenv("GOFLAGS", forceModMod(os.Getenv("GOFLAGS"))); err != nil {
			return ex.Wrapf(err, "forcing module mode for vendored build")
		}

		// A CLI -mod=vendor beats the GOFLAGS=-mod=mod forced above, so the Phase-1
		// build-plan dry run (both the auto-pin and the direct findDeps fallback
		// below call it) would still resolve vendor/ paths without an @version;
		// rewrite it to module mode when vendoring is active so both build phases
		// agree on where the dependency source lives.
		args = rewriteModVendor(subcommand, args)
	}

	if isSetup() {
		logger.InfoContext(ctx, "Setup has already been completed, skipping setup.")
		return nil
	}

	sp := &setupPhase{
		logger:     logger,
		ruleConfig: cmd.String("rules"),
		buildFlags: extractBuildFlags(subcommand, args),
		testMode:   subcommand == subcmdTest,
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
	sp.testPackages = testPackages(buildPkgs, allPkgs)

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

	// Auto-pin generates/updates otel.instrumentation.go file
	var deps []*Dependency
	if sp.ruleConfig == "" && os.Getenv(util.EnvOtelcRules) == "" {
		pinResult, pinErr := autoPin(ctx, moduleDirs, subcommand, args)
		if pinErr != nil {
			return pinErr
		}
		deps = pinResult.AllDeps
	}

	if deps == nil {
		// Find all dependencies of the project being build
		deps, err = findDeps(ctx, subcommand, args)
		if err != nil {
			return err
		}
	}

	// Match the hook code with these dependencies, including dependencies
	// introduced by the generated runtime imports.
	matched, err := sp.matchAllDeps(ctx, deps, moduleDirs, sp.buildFlags)
	if err != nil {
		return err
	}

	if err = sp.validateAndStoreSelectedConfigurationMetadata(matched); err != nil {
		return err
	}

	sp.warnIfNoInstrumentationMatched(matched)

	// Make the imports rules add visible to the go command before the build.
	if err = sp.writeBuildOverlay(ctx, cmd, args, matched); err != nil {
		return err
	}

	// Generate otelc.runtime.go for all packages
	if err = sp.generateRuntimePerPackage(ctx, buildPkgs, matched); err != nil {
		return err
	}

	// Write the matched ruleset to matched.json for further instrument phase
	if err = sp.store(matched); err != nil {
		return err
	}
	return sp.storeInjectedHash(ctx, matched)
}

// writeBuildOverlay writes the overlay that carries the imports the matched
// rules add (see prepareImportOverlay). A standalone otelc setup cannot pass
// it to the build the user runs, so it prints the flag to add to GOFLAGS.
func (sp *setupPhase) writeBuildOverlay(
	ctx context.Context,
	cmd *cli.Command,
	args []string,
	matched []*rule.InstRuleSet,
) error {
	dir := util.GetOtelcWorkDir()
	if c := commandBuildDir(cmd); c != "" {
		if filepath.IsAbs(c) {
			dir = c
		} else {
			dir = filepath.Join(dir, c)
		}
	}
	goroot, err := pkgload.GOROOT(ctx, sp.buildFlags)
	if err != nil {
		return err
	}
	gomodcache, err := pkgload.GOMODCACHE(ctx, sp.buildFlags)
	if err != nil {
		return err
	}
	moduleDir, workFile, err := pkgload.ModuleAndWorkFile(ctx, dir)
	if err != nil {
		return err
	}
	modFile := workFile
	if modFile == "" {
		modFile = userModFilePath(args, dir)
	}
	if modFile == "" {
		modFile = filepath.Join(moduleDir, goModFileName)
	}
	path, runtimeImports, err := sp.prepareImportOverlay(matched, modFile,
		goroot, gomodcache, userOverlayPath(args, dir), util.GetBuildTempDir())
	if err != nil {
		return ex.Wrapf(err, "preparing the overlay for rule-added imports")
	}
	sp.runtimeImports = runtimeImports
	if path != "" && cmd.Name != "go" {
		_, _ = fmt.Fprintf(os.Stderr, "otelc: add -overlay=%s to GOFLAGS for the instrumented build\n", path)
	}
	return nil
}

// warnIfNoInstrumentationMatched reports the combined result. An individual
// matching pass may legitimately find no instrumentation rules.
func (sp *setupPhase) warnIfNoInstrumentationMatched(matched []*rule.InstRuleSet) {
	hasNonInternal := false
	for _, m := range matched {
		if !m.IsInternal() {
			hasNonInternal = true
			break
		}
	}
	if !hasNonInternal {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: no instrumentation will be applied\n")
		sp.Warn("no instrumentation rules matched any dependencies")
	}
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

// coverPkgWork selects the packages of the main modules for coverage, as
// cmd/go does when -cover is given without -coverpkg.
const coverPkgWork = "-coverpkg=work"

// nestedCoverFlags returns the flags that make a nested `go list -export`
// select the same packages for coverage as the build. Without -coverpkg,
// cmd/go covers the main module packages and the packages named on its command
// line. The nested command names the added import, so it would cover that
// import, which the build did not, and the link fails with a fingerprint
// mismatch. goflags are the GOFLAGS tokens and forwarded the flags that
// extractBuildFlags returned; a forwarded flag overrides GOFLAGS.
func nestedCoverFlags(goflags, forwarded []string) []string {
	cover, coverPkg := false, false
	for _, tok := range slices.Concat(goflags, forwarded) {
		name, value, hasValue := strings.Cut(util.UnquoteGoflagsToken(tok), "=")
		switch flagName(name) {
		case flagCover:
			cover = !hasValue
			if hasValue {
				cover, _ = strconv.ParseBool(value)
			}
		case flagCoverpkg:
			coverPkg = true
		}
	}
	if !cover || coverPkg {
		return nil
	}
	return []string{coverPkgWork}
}

// absChangeDir returns args with a relative -C directory made absolute. The
// toolexec processes run in other directories, so they would resolve a
// relative -C against the wrong one when they load packages. It relies on
// classifyArgs so that a value of another flag, such as -tags -C, is left alone.
func absChangeDir(subcommand string, args []string) ([]string, error) {
	result := slices.Clone(args)
	for _, a := range classifyArgs(subcommand, args) {
		if a.FlagName != "-C" || a.Value == "" {
			continue
		}
		prefix := ""
		switch {
		case a.Kind == ArgBuildFlagValue:
		case a.Kind == ArgBuildFlag && a.HasValue:
			name, _, _ := strings.Cut(a.Raw, "=")
			prefix = name + "="
		default:
			continue
		}
		dir, err := filepath.Abs(a.Value)
		if err != nil {
			return nil, ex.Wrapf(err, "resolving -C directory %s", a.Value)
		}
		result[a.Index] = prefix + dir
	}
	return result, nil
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

// leadingDirFlag returns the value of a leading -C flag, which the go command
// requires to be the first flag on its command line, and the number of
// arguments the flag takes. It returns "" and 0 when args do not start with -C.
func leadingDirFlag(args []string) (string, int) {
	// A separate -C takes two arguments: the flag and its value.
	const separateFlagArgs = 2
	if len(args) == 0 {
		return "", 0
	}
	switch {
	case (args[0] == "-C" || args[0] == "--C") && len(args) > 1:
		return args[1], separateFlagArgs
	case strings.HasPrefix(args[0], "-C="):
		return strings.TrimPrefix(args[0], "-C="), 1
	case strings.HasPrefix(args[0], "--C="):
		return strings.TrimPrefix(args[0], "--C="), 1
	}
	return "", 0
}

// commandBuildDir returns the -C directory of the go command that cmd runs
// (otelc go) or prepares (otelc setup), or "" when it has none.
func commandBuildDir(cmd *cli.Command) string {
	args := cmd.Args().Slice()
	if cmd.Name == "go" {
		args = cmd.Args().Tail()
	}
	dir, _ := leadingDirFlag(args)
	return dir
}

// addBuildFlags inserts flags after a leading -C, which the go command requires
// to be the first flag on its command line.
func addBuildFlags(args []string, flags ...string) []string {
	_, insertAt := leadingDirFlag(args)

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
func toolexecBuildArgs(args []string, execPath string, vendored bool, goFlags ...string) ([]string, error) {
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
	if vendored {
		restArgs = rewriteModVendor(subcommand, restArgs)
	}
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

type jsonOutputKey struct{}

// ContextWithJSONOutput records that GOFLAGS turned on -json before otelc
// removed it from GOFLAGS (see util.StripJSONFromGoflags). The instrumented
// build then passes -json on its own command line, so it still reports its
// output as JSON.
func ContextWithJSONOutput(ctx context.Context) context.Context {
	return context.WithValue(ctx, jsonOutputKey{}, true)
}

// JSONOutputFromContext reports whether ctx carries ContextWithJSONOutput.
func JSONOutputFromContext(ctx context.Context) bool {
	jsonOutput, _ := ctx.Value(jsonOutputKey{}).(bool)
	return jsonOutput
}

// buildWithToolexec builds the project with the toolexec mode. vendored is
// passed in by GoBuild: Setup already forced GOFLAGS=-mod=mod, but a CLI
// -mod=vendor beats GOFLAGS, so it still has to be neutralized in the build
// args and forwarded flags below.
func buildWithToolexec(ctx context.Context, cmd *cli.Command, vendored bool) error {
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
	if JSONOutputFromContext(ctx) {
		// Ahead of the user's flags, so a -json=false there still wins, as it
		// does over GOFLAGS.
		goFlags = append(goFlags, flagJSON)
	}
	args, goFlags = withImportOverlay(args, goFlags)
	newArgs, err := toolexecBuildArgs(args, execPath, vendored, goFlags...)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "Running go build with toolexec", "args", newArgs)

	// Tell the sub-process the working directory
	env := os.Environ()
	pwd := util.GetOtelcWorkDir()
	util.Assert(pwd != "", "invalid working directory")
	env = append(env, fmt.Sprintf("%s=%s", util.EnvOtelcWorkDir, pwd))
	// Tell otelc invocations started under the go command (for example from
	// a test) which build locks and work dir this invocation holds.
	env = append(env, nestedBuildEnv(ctx)...)

	// Extract and forward build flags that affect the build context
	// This ensures `go list` resolves archives matching the current build
	subcommand := args[0]
	restArgs := args[1:]
	forwardArgs, err := absChangeDir(subcommand, restArgs)
	if err != nil {
		return err
	}
	buildFlags := extractBuildFlags(subcommand, forwardArgs)
	if vendored {
		buildFlags = rewriteModVendor(subcommand, buildFlags)
	}
	buildFlags = append(buildFlags, nestedCoverFlags(util.SplitGoflags(os.Getenv("GOFLAGS")), buildFlags)...)
	if len(buildFlags) > 0 {
		encoded := util.EncodeBuildFlags(buildFlags)
		env = append(env, fmt.Sprintf("%s=%s", util.EnvOtelcBuildFlags, encoded))
		logger.DebugContext(ctx, "forwarding build flags", "flags", buildFlags)
	}

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
	return withBuildLock(ctx, commandBuildDir(cmd), func(ctx context.Context) error {
		return runGoBuild(ctx, cmd)
	})
}

func runGoBuild(ctx context.Context, cmd *cli.Command) error {
	ctx = contextWithStateManager(ctx, newStateManager())
	logger := util.LoggerFromContext(ctx)

	if err := cleanupDebugArtifacts(); err != nil {
		return ex.Wrapf(err, "cleaning debug artifacts")
	}

	defer func() {
		// Restore backed-up go.mod/go.sum but keep .otelc-build/ for debugging.
		// Users can run `otelc cleanup` to remove it explicitly. Deferred
		// inside the locked body so the restore always runs under the lock.
		if cleanErr := Cleanup(ctx, false); cleanErr != nil {
			logger.DebugContext(ctx, "cleanup failed", "error", cleanErr)
		}
	}()

	statsEnabled := os.Getenv(util.EnvOtelcStats) != ""

	// Setup forces GOFLAGS=-mod=mod for a vendored project (needed for both
	// entry points, otelc setup and otelc go build). vendored is still computed
	// here too so buildWithToolexec can rewrite an explicit CLI -mod=vendor,
	// which beats GOFLAGS.
	pwd := util.GetOtelcWorkDir()
	vendored := vendoringActive(ctx, pwd)

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
	err = buildWithToolexec(ctx, cmd, vendored)
	if err != nil {
		return err
	}
	if statsEnabled {
		logger.InfoContext(ctx, "build stats", "duration", time.Since(buildStart))
	}
	logger.InfoContext(ctx, "Instrumentation completed successfully")
	return nil
}

// hookImports maps each hook package to every package it imports, directly or
// through a chain. A package on a hook's list cannot import that hook back.
func (sp *setupPhase) hookImports(
	ctx context.Context,
	matched []*rule.InstRuleSet,
) (map[string]map[string]bool, error) {
	hookPaths := sp.hookPackagePaths(matched)
	if len(hookPaths) == 0 {
		return map[string]map[string]bool{}, nil
	}

	pkgs, err := pkgload.LoadPackages(
		ctx,
		packages.NeedName|packages.NeedImports|packages.NeedDeps,
		sp.buildFlags,
		false,
		hookPaths...,
	)
	if err != nil {
		return nil, ex.Wrapf(err, "loading hook packages to find their imports")
	}

	result := make(map[string]map[string]bool, len(pkgs))
	var loadErr error
	for _, hook := range pkgs {
		deps := make(map[string]bool)
		packages.Visit([]*packages.Package{hook}, nil, func(p *packages.Package) {
			if len(p.Errors) > 0 && loadErr == nil {
				loadErr = ex.Newf("loading package %q: %v", p.PkgPath, p.Errors[0])
			}
			deps[p.PkgPath] = true
		})
		result[hook.PkgPath] = deps
	}
	if loadErr != nil {
		return nil, loadErr
	}
	return result, nil
}
