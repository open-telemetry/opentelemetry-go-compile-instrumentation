// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

type instrumentPhase struct {
	logger *slog.Logger
	// buildContext evaluates build constraints (GOOS, GOARCH, etc.) for file rules.
	// If nil, defaults to &build.Default.
	buildContext *build.Context
	// The working directory during compilation
	workDir string
	// The importcfg configuration
	importConfig imports.ImportConfig
	// The path to the importcfg file
	importConfigPath string
	// The most recently parsed file. Not necessarily instrumented: files no
	// rule changed are skipped after parsing.
	target *dst.File
	// The parser for the most recently parsed file.
	parser *ast.AstParser
	// The compiling arguments for the target file
	compileArgs []string
	// The target function to be instrumented
	targetFunc *dst.FuncDecl
	// The before trampoline function
	beforeTrampFunc *dst.FuncDecl
	// The after trampoline function
	afterTrampFunc *dst.FuncDecl
	// Variable declarations waiting to be inserted into target source file
	varDecls []dst.Decl
	// The declaration of the hook context, it should be populated later
	hookCtxDecl *dst.GenDecl
	// The methods of the hook context
	hookCtxMethods []*dst.FuncDecl
	// The trampoline jumps to be optimized
	tjumps []*tJump
	// Content identities (see InstFuncRule.Identity) of func rules already
	// applied during this package's instrumentation. Used to de-duplicate rules
	// that resolve to the same identity, which would otherwise emit duplicate
	// trampoline/HookContext declarations and fail to compile. Scoped to the
	// whole package because HookContext declarations accumulate into one globals
	// file across all instrumented source files.
	appliedFuncIdentities map[string]struct{}
	// Local //go:linkname stub names already emitted in this package. Hook
	// stubs are named from the hook path and function (see hookLinkName), so
	// the same hook used in two files is declared once and both trampolines
	// call that name.
	emittedHookDecls map[string]struct{}
	// Raw-rule applications already performed during this package's
	// instrumentation, keyed by the target *dst.FuncDecl. The inner key is
	// rawRuleDedupKey (Identity plus Pattern, Placement, and Imports).
	// Scoped to the function, not the package, because inject_code inserts
	// into a specific function body; a package may have several func init()
	// declarations across files, each of which must receive the injection.
	appliedRawIdentities map[*dst.FuncDecl]map[string]struct{}
	// importNames maps an import path to a package name, resolved
	// during setup instead of guessed from the import path. importNames
	// has no entries when the table is unavailable, such as after an
	// older setup run.
	importNames map[string]string
	// Hook files already parsed via parseHookFileCached, keyed by absolute
	// file path. A hook package directory is typically shared by many func
	// rules (one file implementing dozens of before/after pairs), so caching
	// by file avoids re-parsing it once per rule.
	parsedHookFiles map[string]*dst.File
	// buildUsesIgnoreDirective is true when some package in this build has a
	// //otelc:ignore comment above a call, found during setup.
	buildUsesIgnoreDirective bool
	// methodCallInfo is built lazily by ensureMethodCallInfo.
	methodCallInfo       *methodCallPackageInfo
	methodCallInfoLoaded bool
	// Rules whose application required globals, recorded in application order
	// so writeGlobals can attribute the generated globals file to its contributors.
	globalsContributors []string
}

func (ip *instrumentPhase) Info(msg string, args ...any)  { ip.logger.Info(msg, args...) }
func (ip *instrumentPhase) Error(msg string, args ...any) { ip.logger.Error(msg, args...) }
func (ip *instrumentPhase) Warn(msg string, args ...any)  { ip.logger.Warn(msg, args...) }
func (ip *instrumentPhase) Debug(msg string, args ...any) { ip.logger.Debug(msg, args...) }

func (ip *instrumentPhase) getBuildContext() *build.Context {
	if ip.buildContext != nil {
		return ip.buildContext
	}
	// The go command does not forward -tags to the compiler, so start from
	// build.Default and apply the tags the user passed to the build.
	bctx := build.Default
	bctx.BuildTags = util.ActiveBuildTags()
	ip.buildContext = &bctx
	return ip.buildContext
}

// debugArtifactDir returns the directory under .otelc-build holding this
// package's debug artifacts (.otelc-build/debug/<escaped-package>/).
func (ip *instrumentPhase) debugArtifactDir() string {
	modPath := util.FindFlagValue(ip.compileArgs, "-p")
	pkgDir := util.EscapePackagePath(modPath)
	return util.GetBuildTemp(filepath.Join("debug", pkgDir))
}

// keepForDebug keeps the the file to .otelc-build directory for debugging
func (ip *instrumentPhase) keepForDebug(name string) {
	dest := filepath.Join(ip.debugArtifactDir(), filepath.Base(name))
	err := util.CopyFile(name, dest)
	if err != nil { // error is tolerable here as this is only for debugging
		ip.Warn("failed to save modified file", "dest", dest, "error", err)
	}
}

func stripCompleteFlag(args []string) []string {
	for i, arg := range args {
		if arg == "-complete" {
			res := slices.Clone(args)
			return slices.Delete(res, i, i+1)
		}
	}
	return args
}

// resolvePackageName reads the package clause from the compile command's own
// sources. Setup's build-plan dry run lists no readable source for the
// go-test-generated main, so setup leaves the rule set unnamed and this method
// recovers it (see #1348). Returns an empty name if the command lists no
// readable Go file.
func (ip *instrumentPhase) resolvePackageName() (string, error) {
	for _, arg := range ip.compileArgs {
		if !util.IsGoFile(arg) || !util.PathExists(arg) {
			continue
		}
		name, err := ast.ParsePackageName(arg)
		if err != nil {
			return "", ex.Wrapf(err, "parsing package clause from %s", arg)
		}
		return name, nil
	}
	return "", nil
}

// ensurePackageName fills in a rule set that setup left unnamed. It must run
// before instrument(), because file rules need the name and compileArgs still
// holds only the original sources at that point.
func (ip *instrumentPhase) ensurePackageName(matched *rule.InstRuleSet) error {
	if matched.PackageName != "" {
		return nil
	}
	name, err := ip.resolvePackageName()
	if err != nil {
		return err
	}
	if name != "" {
		matched.SetPackageName(name)
		ip.Debug("Resolved package name from compile command", "package", name)
	}
	return nil
}

func interceptCompile(ctx context.Context, args []string) ([]string, error) {
	// Read compilation output directory
	target := util.FindFlagValue(args, "-o")
	util.Assert(target != "", "missing -o flag value")

	// Extract -importcfg flag
	importCfgPath := util.FindFlagValue(args, "-importcfg")

	ip := &instrumentPhase{
		logger:           util.LoggerFromContext(ctx),
		workDir:          filepath.Dir(target),
		compileArgs:      args,
		importConfigPath: importCfgPath,
	}

	// Parse existing importcfg if present
	if importCfgPath != "" {
		imports, err := imports.ParseImportCfg(importCfgPath)
		if err != nil {
			return nil, err
		}
		ip.importConfig = imports
	}

	// Load matched hook rules from setup phase
	allSet, err := ip.load()
	if err != nil {
		return nil, err
	}
	ip.buildUsesIgnoreDirective = anyRuleSetHasIgnoredCallFiles(allSet)

	// Load the import name table, if one exists. A missing table is
	// not fatal.
	ip.importNames, err = loadImportNames()
	if err != nil {
		return nil, err
	}

	// Check if the current compile command matches the rules.
	matched := ip.match(allSet, args)
	if !matched.IsEmpty() {
		if err = ip.ensurePackageName(matched); err != nil {
			return nil, err
		}

		ip.Info("Instrument package", "rules", matched, "args", args)
		// Okay, this package should be instrumented.
		err = ip.instrument(ctx, matched)
		if err != nil {
			return nil, ex.Wrapf(err, "instrumenting package %s", matched.ModulePath)
		}

		// Strip -complete flag as we may insert some hook points that are
		// not ready yet, i.e. they don't have function body
		ip.compileArgs = stripCompleteFlag(ip.compileArgs)
		ip.Info("Run instrumented command", "args", ip.compileArgs)
	}

	return ip.compileArgs, nil
}

// updateImportConfig updates the importcfg file with new imports that were added during instrumentation.
func (ip *instrumentPhase) updateImportConfig(ctx context.Context, newImports map[string]string) error {
	if ip.importConfigPath == "" {
		// No importcfg file, skip (shouldn't happen in normal builds)
		return nil
	}

	// Initialize PackageFile map if nil
	if ip.importConfig.PackageFile == nil {
		ip.importConfig.PackageFile = make(map[string]string)
	}

	pgoFlags, pgoErr := ip.pgoBuildFlags()
	if pgoErr != nil {
		return pgoErr
	}
	var updated bool
	for _, importPath := range newImports {
		if importPath == "unsafe" || importPath == "C" {
			// unsafe is built-in, C is the cgo pseudo-package; neither has an archive file
			continue
		}

		if _, exists := ip.importConfig.PackageFile[importPath]; exists {
			// Already have this import
			continue
		}

		restore, err := ip.enterNestedResolution(importPath)
		if err != nil {
			return err
		}
		// Resolve package archive location, passing build flags to match the current build context
		buildFlags := append(util.GetBuildFlags(), pgoFlags...)
		archives, err := pkgload.ResolveExportFiles(ctx, importPath, buildFlags...)
		restore()
		if err != nil {
			return ex.Wrapf(err, "resolving %q", importPath)
		}

		for pkg, archive := range archives {
			if _, exists := ip.importConfig.PackageFile[pkg]; !exists {
				ip.Debug("Adding import to importcfg", "package", pkg, "archive", archive)
				ip.importConfig.PackageFile[pkg] = archive
				updated = true
			}
		}
	}

	if !updated {
		return nil
	}

	if err := ip.importConfig.WriteFile(ip.importConfigPath); err != nil {
		return err
	}

	ip.Info("Updated importcfg", "path", ip.importConfigPath)

	// Track added imports for the link phase
	if err := trackAddedImports(ip.importConfig.PackageFile); err != nil {
		ip.Warn("failed to track added imports for link phase", "error", err)
		// Non-fatal: link phase may still work if imports were already present
	}

	return nil
}

// pgoBuildFlags returns the -pgo flag that makes a nested go list compile the
// added imports with the profile whose path storePGOProfile recorded during
// setup, when this compile uses one.
func (ip *instrumentPhase) pgoBuildFlags() ([]string, error) {
	return pgoBuildFlagsFor(ip.compileArgs)
}

// pgoBuildFlagsFor returns the -pgo flag that makes a nested go list compile
// the added imports of the command in args with the profile whose path
// storePGOProfile recorded during setup, when that command uses one. Without
// it the nested build compiles them unprofiled, and the link rejects the
// mix of export data with a fingerprint mismatch.
//
// A compile command decides from -pgoprofile, which the toolchain adds
// exactly when it profiles that compile. A link command never carries
// -pgoprofile: the profile shapes compile actions, not the link. Link-side
// resolution therefore follows the profile setup recorded, which setup
// rewrites before every build, empty when the build uses none.
func pgoBuildFlagsFor(args []string) ([]string, error) {
	if !util.IsLinkCommandWithArgs(args) && util.FindFlagValue(args, "-pgoprofile") == "" {
		return nil, nil
	}
	f := util.GetPGOProfileFile()
	profilePath, err := os.ReadFile(f)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, ex.Wrapf(err, "failed to read PGO profile from file %s", f)
	}
	if len(profilePath) == 0 {
		return nil, nil
	}
	// go/packages passes -pgo=off; a later -pgo flag overrides it.
	return []string{"-pgo=" + string(profilePath)}, nil
}

// resolution is a compile waiting on a nested build to resolve an import a rule
// added to it.
type resolution struct {
	pkg   string
	added string
}

// maxResolutionChain bounds how deep added imports may nest. Realistic rules
// close onto a waiting package within two levels; anything past this bound is
// an adversarial rule set evading cycle detection.
const maxResolutionChain = 32

// enterNestedResolution records, for the nested build about to resolve added,
// that the package being compiled waits on it. It fails when added is a
// package already waiting further up the chain: the nested build would compile
// it again, add the same import, and never end. Go cannot catch this itself,
// because a nested build only sees each package's original imports. It also
// fails when the chain grows past maxResolutionChain, which rules could
// otherwise do without ever closing a cycle. The returned function restores
// the previous chain.
func (ip *instrumentPhase) enterNestedResolution(added string) (func(), error) {
	prev, hadPrev := os.LookupEnv(util.EnvOtelcNestedResolving)
	chain := parseResolutionChain(prev)
	chain = append(chain, resolution{pkg: util.FindFlagValue(ip.compileArgs, "-p"), added: added})
	for _, r := range chain {
		if r.pkg == added {
			return nil, ex.Newf("rules add an import cycle: %s", describeResolutionChain(chain))
		}
	}
	if len(chain) > maxResolutionChain {
		return nil, ex.Newf(
			"rules add imports nested deeper than %d: %s",
			maxResolutionChain,
			describeResolutionChain(chain),
		)
	}

	if err := os.Setenv(util.EnvOtelcNestedResolving, encodeResolutionChain(chain)); err != nil {
		return nil, ex.Wrapf(err, "setting %s", util.EnvOtelcNestedResolving)
	}
	return func() {
		// The variable name is fixed and valid, so this cannot meaningfully
		// fail; if it did, a leaked chain can only cause a spurious cycle error
		// later in this same process, never unbounded nesting.
		if hadPrev {
			_ = os.Setenv(util.EnvOtelcNestedResolving, prev)
		} else {
			_ = os.Unsetenv(util.EnvOtelcNestedResolving)
		}
	}, nil
}

func parseResolutionChain(s string) []resolution {
	if s == "" {
		return nil
	}
	entries := strings.Split(s, ",")
	chain := make([]resolution, 0, len(entries))
	for _, entry := range entries {
		pkg, added, _ := strings.Cut(entry, ">")
		chain = append(chain, resolution{pkg: pkg, added: added})
	}
	return chain
}

// encodeResolutionChain encodes chain as comma-separated "pkg>added" entries.
// No escaping is needed: cmd/go rejects ",", ">", and spaces in import paths
// ("malformed import path ... invalid char"), so the separators cannot occur
// inside either field. Keep it that way rather than switching to an escaped
// format such as JSON.
func encodeResolutionChain(chain []resolution) string {
	entries := make([]string, len(chain))
	for i, r := range chain {
		entries[i] = r.pkg + ">" + r.added
	}
	return strings.Join(entries, ",")
}

func describeResolutionChain(chain []resolution) string {
	steps := make([]string, len(chain))
	for i, r := range chain {
		steps[i] = r.pkg + " adds " + r.added
	}
	return strings.Join(steps, "; ")
}

// trackAddedImports saves the resolved package files to a per-process tracking file.
// During the link phase, all per-process files will be merged.
// Each compile process writes to its own file to avoid inter-process race conditions.
func trackAddedImports(packages map[string]string) error {
	if len(packages) == 0 {
		return nil
	}

	// Write to process-specific file (no locking needed)
	filePath := util.GetAddedImportsFileForProcess()

	data, err := json.MarshalIndent(packages, "", "  ")
	if err != nil {
		return ex.Wrapf(err, "marshaling added imports")
	}

	if err = os.WriteFile(filePath, data, 0o600); err != nil {
		return ex.Wrapf(err, "writing imports file")
	}

	return nil
}

// addedImportFiles returns the per-process import tracking files written during
// compilation. It scans the build temp directory instead of globbing so that a
// workspace whose path contains glob meta-characters (for example square
// brackets) still resolves its files.
func addedImportFiles() ([]string, error) {
	dir := util.GetBuildTempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, ex.Wrapf(err, "reading build temp directory %q", dir)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "added_imports.") || !strings.HasSuffix(name, ".json") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	return files, nil
}

// CleanupImportTrackingFiles removes import tracking files from previous builds.
// Should be called at the start of a new build to clean up stale files from prior runs.
// This is exported for use by the setup phase.
func CleanupImportTrackingFiles() {
	files, err := addedImportFiles()
	if err != nil {
		return
	}

	for _, file := range files {
		_ = os.Remove(file) // Best effort cleanup
	}
}

// CleanupDebugArtifacts removes debug artifacts from previous builds.
// Should be called at the start of a build under debug mode to clean up
// stale diffs and debug files from prior runs.
// This is exported for use by the setup phase.
func CleanupDebugArtifacts() error {
	return os.RemoveAll(util.GetBuildTemp("debug"))
}

// loadAddedImports discovers and merges all per-process import tracking files.
func loadAddedImports(ctx context.Context) (map[string]string, error) {
	logger := util.LoggerFromContext(ctx)

	// Find all per-process import files
	files, err := addedImportFiles()
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		// No imports were added during compilation
		return make(map[string]string), nil
	}

	// Merge all files
	merged := make(map[string]string)
	for _, filePath := range files {
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			// Log warning but continue with other files
			logger.WarnContext(
				ctx,
				"failed to read import file",
				"path",
				filePath,
				"error",
				readErr,
			)
			continue
		}

		var imports map[string]string
		if unmarshalErr := json.Unmarshal(data, &imports); unmarshalErr != nil {
			logger.WarnContext(
				ctx,
				"failed to parse import file",
				"path",
				filePath,
				"error",
				unmarshalErr,
			)
			continue
		}

		// Merge into result
		maps.Copy(merged, imports)
	}

	return merged, nil
}

// interceptLink updates the link-time importcfg with packages added during compilation.
func interceptLink(ctx context.Context, args []string) ([]string, error) {
	logger := util.LoggerFromContext(ctx)

	// Extract -importcfg flag for link
	importCfgPath := util.FindFlagValue(args, "-importcfg")
	if importCfgPath == "" {
		// No importcfg, nothing to update
		return args, nil
	}

	// Load imports that were added during compilation. A failure is not
	// fatal: matched.json still says which imports rules add, and
	// resolveRuleAddedImports recovers them below.
	addedImports, err := loadAddedImports(ctx)
	if err != nil {
		logger.WarnContext(ctx, "failed to load added imports for link phase", "error", err)
		addedImports = nil
	}

	// Parse the link importcfg
	linkConfig, err := imports.ParseImportCfg(importCfgPath)
	if err != nil {
		return nil, err
	}

	if linkConfig.PackageFile == nil {
		linkConfig.PackageFile = make(map[string]string)
	}

	// Add missing packages from compilation phase
	var updated bool
	for pkg, archive := range addedImports {
		if _, exists := linkConfig.PackageFile[pkg]; !exists {
			logger.DebugContext(ctx, "Adding package to link importcfg", "package", pkg, "archive", archive)
			linkConfig.PackageFile[pkg] = archive
			updated = true
		}
	}

	// The merge above depends on a side effect of compile actions that a
	// build cache hit skips: a cached compile writes no tracking file, so on
	// a warm GOCACHE the link misses the imports rules added to the cached
	// packages. matched.json survives that, because setup rewrites it
	// deterministically before every build, so recover the imports from it
	// and resolve whatever the link does not list yet.
	ruleUpdated, err := resolveRuleAddedImports(ctx, args, &linkConfig)
	if err != nil {
		return nil, err
	}
	updated = updated || ruleUpdated

	if !updated {
		return args, nil
	}

	if err = linkConfig.WriteFile(importCfgPath); err != nil {
		return nil, err
	}

	logger.InfoContext(ctx, "Updated link importcfg", "path", importCfgPath, "added", len(addedImports))

	// Note: We don't clean up tracking files here because multi-link builds
	// (e.g., go build ./cmd/...) need the files available for all link steps.
	// Cleanup happens at the start of the next build via CleanupImportTrackingFiles.

	return args, nil
}

// resolveRuleAddedImports completes the link importcfg with the imports that
// instrumentation rules can add, resolved the same way the compile phase
// resolves them: a nested go list -export that shares the build cache keys.
// It reports whether the import config changed.
func resolveRuleAddedImports(ctx context.Context, linkArgs []string, linkConfig *imports.ImportConfig) (bool, error) {
	candidates := ruleAddedImports(ctx, linkConfig)
	if len(candidates) == 0 {
		return false, nil
	}

	logger := util.LoggerFromContext(ctx)
	missing := make([]string, 0, len(candidates))
	for path := range candidates {
		if path == unsafePackageName || path == "C" {
			// unsafe is built-in, C is the cgo pseudo-package; neither has an archive file
			continue
		}
		if _, exists := linkConfig.PackageFile[path]; !exists {
			missing = append(missing, path)
		}
	}
	if len(missing) == 0 {
		return false, nil
	}
	// Sorting keeps the go list output and the importcfg edits stable.
	slices.Sort(missing)

	pgoFlags, err := pgoBuildFlagsFor(linkArgs)
	if err != nil {
		return false, err
	}
	buildFlags := append(util.GetBuildFlags(), pgoFlags...)
	archives, failed, err := pkgload.ResolveExportFilesBatch(ctx, missing, buildFlags...)
	if err != nil {
		return false, err
	}
	for path, loadErr := range failed {
		// A candidate the rules declare but that this build never compiles can
		// be unresolvable, for example a hook package that only builds on
		// another platform. Skip it: the link fails with go's own error when
		// the import was genuinely needed.
		logger.WarnContext(ctx, "skipping unresolvable rule-added import for the link",
			"import", path, "error", loadErr)
	}

	var updated bool
	for pkg, archive := range archives {
		if _, exists := linkConfig.PackageFile[pkg]; exists {
			continue
		}
		logger.DebugContext(ctx, "Adding rule-added package to link importcfg", "package", pkg, "archive", archive)
		linkConfig.PackageFile[pkg] = archive
		updated = true
	}
	return updated, nil
}

// ruleAddedImports collects the import paths instrumentation rules can add
// to a package's compile: the imports every rule declares, plus the imports of
// the source files add_file rules insert. It reads matched.json, which setup
// wrote before the build, so the answer never depends on a compile running.
// Only rule sets whose target package takes part in the link described by
// linkConfig contribute: a link importcfg lists every package the binary
// links, including the linked root itself, so the imports of another
// binary's rules are never resolved for this one. The generated test main
// is the one root a link importcfg never lists, so a set for it is always
// kept. The result stays a superset of what the link needs: a rule can
// declare an import the build never uses, and resolveRuleAddedImports skips
// unresolvable candidates.
func ruleAddedImports(ctx context.Context, linkConfig *imports.ImportConfig) map[string]struct{} {
	logger := util.LoggerFromContext(ctx)

	content, err := os.ReadFile(util.GetMatchedRuleFile())
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logger.WarnContext(ctx, "failed to read matched rules for link imports", "error", err)
		}
		return nil
	}
	var sets []*rule.InstRuleSet
	if err = json.Unmarshal(content, &sets); err != nil {
		logger.WarnContext(ctx, "failed to parse matched rules for link imports", "error", err)
		return nil
	}

	// The same constraint context the compile phase applies: build.Default
	// with the tags the user passed to the build, so add_file sources the
	// active constraints exclude are skipped exactly as the compile skips
	// them. See instrumentPhase.getBuildContext.
	bctx := build.Default
	bctx.BuildTags = util.ActiveBuildTags()

	paths := make(map[string]struct{})
	for _, set := range sets {
		if _, linked := linkConfig.PackageFile[set.ModulePath]; !linked && set.ModulePath != "main" {
			// The target package takes no part in this link: another binary's
			// rules, or a package this build never compiled. The generated
			// test main has import path "main", which no importcfg lists.
			continue
		}
		addImportsOf(paths, set.RawRules)
		addImportsOf(paths, set.FuncRules)
		addImportsOf(paths, set.StructRules)
		addImportsOf(paths, set.CallRules)
		addImportsOf(paths, set.LitRules)
		addImportsOf(paths, set.DirectiveRules)
		addImportsOf(paths, set.DeclRules)
		for _, fileRule := range set.FileRules {
			for _, path := range fileRuleImports(ctx, &bctx, fileRule) {
				paths[path] = struct{}{}
			}
		}
	}
	return paths
}

// addImportsOf adds the declared imports of every rule in a file-keyed rule
// map. It is generic because every rule kind embeds InstBaseRule and promotes
// its Base accessor.
func addImportsOf[T interface{ Base() *rule.InstBaseRule }](
	paths map[string]struct{},
	rules map[string][]T,
) {
	for _, fileRules := range rules {
		for _, r := range fileRules {
			for _, path := range r.Base().Imports {
				paths[path] = struct{}{}
			}
		}
	}
}

// fileRuleImports returns the import paths an add_file rule's source declares.
// The source joins a target package's compile only through toolexec, so its
// imports are as invisible to go's build graph as the imports rules declare.
// The source must satisfy the active build constraints, the same check the
// compile applies: a source the constraints exclude is never added to a
// compile, so its imports are not candidates either. An "ignore" tag counts
// as satisfied, because otelc compiles files ordinary builds ignore.
func fileRuleImports(ctx context.Context, bctx *build.Context, fileRule *rule.InstFileRule) []string {
	if fileRule.ResolvedPath == "" {
		return nil
	}
	file := filepath.Join(fileRule.ResolvedPath, fileRule.File)
	src, err := os.ReadFile(file)
	if err != nil {
		util.LoggerFromContext(ctx).WarnContext(ctx,
			"failed to read add_file rule source for link imports", "file", file, "error", err)
		return nil
	}
	match, err := ast.MatchFileConstraints(bctx, fileRule.ResolvedPath, fileRule.File, src)
	if err != nil {
		util.LoggerFromContext(ctx).WarnContext(ctx,
			"failed to match add_file rule constraints for link imports", "file", file, "error", err)
		return nil
	}
	if !match {
		return nil
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), file, src, parser.ImportsOnly)
	if err != nil {
		util.LoggerFromContext(ctx).WarnContext(ctx,
			"failed to parse add_file rule source for link imports", "file", file, "error", err)
		return nil
	}
	paths := make([]string, 0, len(parsed.Imports))
	for _, imp := range parsed.Imports {
		path, unquoteErr := strconv.Unquote(imp.Path.Value)
		if unquoteErr != nil {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

const vetToolName = "vet"

func interceptVet(ctx context.Context, args []string) ([]string, error) {
	if len(args) == 0 {
		return args, nil
	}

	configPath := args[len(args)-1]
	if filepath.Base(configPath) != "vet.cfg" {
		util.LoggerFromContext(ctx).DebugContext(
			ctx,
			"vet invocation missing expected vet.cfg argument",
			"args",
			args,
		)
		return args, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, ex.Wrapf(err, "reading vet config")
	}

	var config map[string]json.RawMessage
	if err = json.Unmarshal(data, &config); err != nil {
		return nil, ex.Wrapf(err, "parsing vet config")
	}

	// A config without GoFiles has no source list to rewrite, so leave the
	// command alone rather than failing the build. Indexing a missing key
	// yields a nil RawMessage, which Unmarshal reports as malformed JSON.
	raw, ok := config["GoFiles"]
	if !ok {
		return args, nil
	}

	var goFiles []string
	if err = json.Unmarshal(raw, &goFiles); err != nil {
		return nil, ex.Wrapf(err, "parsing GoFiles from vet config")
	}

	var updated bool
	for i, file := range goFiles {
		if !strings.HasSuffix(file, ".cgo1.go") {
			continue
		}
		vetFile := cgoVetSourcePath(file)
		if _, statErr := os.Stat(vetFile); statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			return nil, ex.Wrapf(statErr, "checking preserved cgo source")
		}
		goFiles[i] = vetFile
		updated = true
	}
	if !updated {
		return args, nil
	}

	config["GoFiles"], err = json.Marshal(goFiles)
	if err != nil {
		return nil, ex.Wrapf(err, "encoding GoFiles for vet config")
	}
	data, err = json.Marshal(config)
	if err != nil {
		return nil, ex.Wrapf(err, "encoding vet config")
	}
	if err = os.WriteFile(configPath, data, 0o600); err != nil {
		return nil, ex.Wrapf(err, "writing vet config")
	}

	return args, nil
}

func cgoVetSourcePath(path string) string {
	return strings.TrimSuffix(path, ".cgo1.go") + ".otelc.vet.go"
}

// toolVersionLine appends an otelc marker to a `tool -V=full` line so the tool
// ID (and every build cache key derived from it) differs from a plain build.
// The rules hash is included so editing the config invalidates cached
// artifacts too. For devel toolchains, Go uses only the content ID in the
// trailing `buildID=...` field, so append the marker to that field's value.
func toolVersionLine(line, rulesHash string) string {
	hasBuildID := strings.Contains(line, " buildID=")
	marker := "otelc@" + util.Version
	if rulesHash != "" {
		if hasBuildID {
			marker += "+" + rulesHash
		} else {
			marker += "/" + rulesHash
		}
	}
	if hasBuildID {
		return line + "+" + marker
	}
	return line + " " + marker
}

// markedToolVersion turns a tool's raw `-V=full` output into the line otelc
// reports in its place: the version with an otelc marker, plus the current
// matched-rules hash when one exists.
func markedToolVersion(rawOutput string) string {
	var rulesHash string
	if content, err := os.ReadFile(util.GetMatchedRuleFile()); err == nil {
		// Build tags decide which add_file rule files apply, so a tagged build
		// must not reuse artifacts compiled without those tags.
		if tags := util.ActiveBuildTags(); len(tags) > 0 {
			content = append(content, "\ntags="+strings.Join(tags, ",")...)
		}
		sum := sha256.Sum256(content)
		rulesHash = hex.EncodeToString(sum[:8])
	}
	return toolVersionLine(strings.TrimSpace(rawOutput), rulesHash)
}

// interceptToolVersion handles the `tool -V=full` probe go uses to compute
// tool IDs, printing the tool's own version line with an otelc marker added.
func interceptToolVersion(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec // args come from the go toolchain
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return ex.Wrapf(err, "running %v", args)
	}

	// The line goes to stdout: it is the answer go itself is waiting for.
	_, err = os.Stdout.WriteString(markedToolVersion(string(out)) + "\n")
	if err != nil {
		return ex.Wrapf(err, "writing tool version")
	}
	return nil
}

// Toolexec is the entry point of the toolexec command. It intercepts all the
// commands(link, compile, asm, etc) during build process. Our responsibility is
// to find out the compile command we are interested in and run it with the
// instrumented code, and ensure the link command has all necessary dependencies.
// nested (see EnvOtelcNestedToolexec) means this runs inside a go command
// another otelc spawned, such as the `go list -export` that resolves an import
// a rule added. Those invocations instrument compiles and run every other
// command unchanged.
func Toolexec(ctx context.Context, args []string, nested bool) error {
	if len(args) == 0 {
		return ex.New("no tool command specified")
	}

	// Use slice-based detection to correctly handle tool paths with spaces
	// (common on Windows, e.g., "C:\Program Files\Go\pkg\tool\...")

	// go derives each tool's ID (an input to every build cache key) from
	// `tool -V=full` run through toolexec. Answer it with an otelc marker
	// appended so instrumented artifacts never share cache entries with plain
	// builds; instrumentation changes compile output without changing any
	// input go hashes.
	if len(args) == 2 && args[1] == "-V=full" { //nolint:gosec // false positive: args[1] is guarded by len(args) == 2
		return interceptToolVersion(ctx, args)
	}

	// A nested build answers `-V=full` the same way as the outer one, so both
	// share cache keys, and it must compile the same instrumented output: a
	// plain compile there would be stored under the key of the instrumented
	// one, and a package compiled against it would not match the archive the
	// outer build links. A cycle of added imports would nest forever, and
	// enterNestedResolution stops it.
	var err error
	switch {
	case !nested:
		args, err = interceptToolCommand(ctx, args)
	case util.IsCompileCommandWithArgs(args):
		args, err = interceptCompile(ctx, args)
	}
	if err != nil {
		return err
	}

	// Run the command
	if os.Getenv(util.EnvOtelcStats) == "" {
		return util.RunCmd(ctx, args...)
	}
	tool := filepath.Base(args[0])
	pkg := util.FindFlagValue(args, "-p")
	start := time.Now()
	err = util.RunCmd(ctx, args...)
	elapsed := time.Since(start)
	util.LoggerFromContext(ctx).InfoContext(ctx, "toolexec stats",
		"tool", tool,
		"package", pkg,
		"duration", elapsed,
	)
	return err
}

// interceptToolCommand rewrites the compile, link, and vet commands otelc cares
// about; every other tool invocation is returned unchanged.
func interceptToolCommand(ctx context.Context, args []string) ([]string, error) {
	// Intercept compile commands for instrumentation
	if util.IsCompileCommandWithArgs(args) {
		return interceptCompile(ctx, args)
	}
	// Intercept link commands to update importcfg with added dependencies
	if util.IsLinkCommandWithArgs(args) {
		return interceptLink(ctx, args)
	}
	if len(args) > 0 && strings.TrimSuffix(filepath.Base(args[0]), ".exe") == vetToolName {
		return interceptVet(ctx, args)
	}
	return args, nil
}

// nestedToolexecGoflagsToken builds the GOFLAGS-safe -toolexec token for
// execPath. Split out from EnableNestedToolexec so the error paths (only
// reachable when execPath itself contains quote characters) are testable
// without depending on os.Executable.
func nestedToolexecGoflagsToken(execPath string) (string, error) {
	// Quoted twice on purpose: the inner quote keeps a spaced path intact when
	// cmd/go splits the -toolexec value, the outer one keeps the whole flag a
	// single token when cmd/go splits GOFLAGS.
	innerFlag, err := util.BuildToolexecFlag(execPath)
	if err != nil {
		// BuildToolexecFlag's own error is already stackful and already names
		// execPath, so there is nothing to add here; see #1231.
		return "", err
	}
	toolexecFlag, err := util.QuoteGoflagsToken(innerFlag)
	if err != nil {
		// Reachable whenever the finished flag holds both quote characters:
		// the inner quoting supplies one kind to keep a spaced path together,
		// and a quote already in execPath supplies the other. GOFLAGS is split
		// by cmd/internal/quoted.Split, which has no escape syntax, so such a
		// token simply cannot be represented there.
		return "", ex.Wrapf(err, "otelc's own path %q cannot be passed to nested "+
			"go commands through GOFLAGS, because quoting it there would need "+
			"both quote characters and GOFLAGS has no way to escape either; "+
			"reinstall otelc under a path without quote characters", execPath)
	}
	return toolexecFlag, nil
}

// executablePath resolves the path to the current executable. It is a variable
// so tests can simulate paths that trigger quoting errors.
//
//nolint:gochecknoglobals // test seam
var executablePath = os.Executable

// EnableNestedToolexec points GOFLAGS at this executable in nested mode, so go
// commands this process spawns (e.g. `go list -export`) run through otelc,
// instrument compiles the same way and share this build's cache keys. Any
// existing -toolexec was stripped at startup. Must only be called from the
// real otelc binary, since os.Executable is what nested go commands will run.
//
// A -gcflags value without a package pattern applies only to a go command's
// command-line packages. The outer build applied it to its targets, whereas
// nested `go list -export` targets the added import, so strip such values here.
//
// Nested mode assumes its go commands are `go list -export` spawned from
// updateImportConfig: those run compile (instrumented), asm, cgo, and pack,
// and answer -V=full probes, but never link or vet. A new go command spawned
// during the instrumentation phase must be audited for the tools it runs
// before it can rely on nested mode.
func EnableNestedToolexec() error {
	execPath, err := executablePath()
	if err != nil {
		return ex.Wrapf(err, "resolving otelc executable path")
	}
	toolexecFlag, err := nestedToolexecGoflagsToken(execPath)
	if err != nil {
		return err
	}
	goflags := strings.TrimSpace(util.StripUnpatternedGcflagsFromGoflags(os.Getenv("GOFLAGS")) + " " + toolexecFlag)
	if err = os.Setenv("GOFLAGS", goflags); err != nil {
		return ex.Wrapf(err, "setting GOFLAGS for nested go commands")
	}
	if err = os.Setenv(util.EnvOtelcNestedToolexec, "1"); err != nil {
		return ex.Wrapf(err, "setting %s", util.EnvOtelcNestedToolexec)
	}
	return nil
}
