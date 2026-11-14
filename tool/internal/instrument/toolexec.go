// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/build"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

type instrumentPhase struct {
	diagnosticSource string
	// ignoredCallSites counts the matching call sites skipped for
	// //otelc:ignore, so diagnostics can tell an ignored call from no call.
	ignoredCallSites int
	logger           *slog.Logger
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
	// resolveExportFiles looks up go list -export files for type-checking;
	// nil means pkgload.ResolveExportFilesBatch. Tests replace it.
	resolveExportFiles func(
		ctx context.Context, importPaths []string, buildFlags ...string,
	) (map[string]string, map[string]error, error)
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

// updateImportConfig checks that every import instrumentation added is in the
// compile's importcfg. Setup makes the go command see those imports through
// -overlay, so the go command already compiled them and listed them.
func (ip *instrumentPhase) updateImportConfig(newImports map[string]string) error {
	if ip.importConfigPath == "" {
		// No importcfg file, skip (shouldn't happen in normal builds)
		return nil
	}
	for _, importPath := range slices.Sorted(maps.Values(newImports)) {
		if importPath == unsafePackageName || importPath == "C" {
			// unsafe is built-in, C is the cgo pseudo-package; neither has an archive file
			continue
		}
		if mapped, ok := ip.importConfig.ImportMap[importPath]; ok {
			importPath = mapped
		}
		if _, ok := ip.importConfig.PackageFile[importPath]; ok {
			continue
		}
		if slices.ContainsFunc(ip.compileArgs, func(arg string) bool { return filepath.Base(arg) == "_testmain.go" }) {
			return ex.Newf("rule import %q cannot be added to the testmain that go test generates: the go "+
				"command fixes its imports; reach the package through //go:linkname instead", importPath)
		}
		if changed, _ := util.ModFileChangedSinceSetup(); changed {
			return ex.Newf("rule import %q is not in the importcfg of %s: go.mod changed since otelc setup "+
				"wrote the overlay; run otelc setup again", importPath, util.FindFlagValue(ip.compileArgs, "-p"))
		}
		return ex.Newf("rule import %q is not in the importcfg of %s; the build is missing otelc's "+
			"-overlay (run it through otelc go, or add the -overlay that otelc setup printed to GOFLAGS)",
			importPath, util.FindFlagValue(ip.compileArgs, "-p"))
	}
	return nil
}

// CleanupDebugArtifacts removes debug artifacts from previous builds.
// Should be called at the start of a build under debug mode to clean up
// stale diffs and debug files from prior runs.
// This is exported for use by the setup phase.
func CleanupDebugArtifacts() error {
	return os.RemoveAll(util.GetBuildTemp("debug"))
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
		// go never sees the packages rules add, so their hash stands in. The
		// tool ID is shared by every compile, so an edit to one injected
		// package rebuilds the whole instrumented build, not only the
		// packages that import it. Missing an edit would put stale code in
		// the binary, so the coarser invalidation is the accepted cost.
		if injected, injErr := os.ReadFile(util.GetInjectedHashFile()); injErr == nil && len(injected) > 0 {
			content = append(content, "\ninjected="+string(injected)...)
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
// existing -toolexec was stripped at startup, so every toolexec invocation
// calls it, nested ones too. Must only be called from the real otelc binary,
// since os.Executable is what nested go commands will run.
//
// A -gcflags value without a package pattern applies only to a go command's
// command-line packages. The outer build applied it to its targets, whereas
// nested `go list -export` targets the added import, so strip such values here.
//
// Nested mode assumes its go commands are `go list -export` spawned from
// updateImportConfig or exportDataFiles: those run compile (instrumented),
// asm, cgo, pack, and from Go 1.28 export (passed through), and answer
// -V=full probes, but never link or vet. A new go command spawned
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
