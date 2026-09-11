// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/gofrs/flock"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/util"
)

type instrumentPhase struct {
	logger *slog.Logger
	// The working directory during compilation
	workDir string
	// The importcfg configuration
	importConfig imports.ImportConfig
	// The path to the importcfg file
	importConfigPath string
	// The target file to be instrumented
	target *dst.File
	// The parser for the target file
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
	// Hook files already parsed via parseHookFileCached, keyed by absolute
	// file path. A hook package directory is typically shared by many func
	// rules (one file implementing dozens of before/after pairs), so caching
	// by file avoids re-parsing it once per rule.
	parsedHookFiles map[string]*dst.File
	// Rules whose application required globals, recorded in application order
	// so writeGlobals can attribute the generated globals file to its contributors.
	globalsContributors []string
}

func (ip *instrumentPhase) Info(msg string, args ...any)  { ip.logger.Info(msg, args...) }
func (ip *instrumentPhase) Error(msg string, args ...any) { ip.logger.Error(msg, args...) }
func (ip *instrumentPhase) Warn(msg string, args ...any)  { ip.logger.Warn(msg, args...) }
func (ip *instrumentPhase) Debug(msg string, args ...any) { ip.logger.Debug(msg, args...) }

const debugSessionsDirName = "sessions"

func isWrapperMode() bool {
	return strings.HasPrefix(os.Getenv(util.EnvOtelcBuildSession), "wrapper_")
}

// debugArtifactDir returns the directory under .otelc-build holding this
// package's debug artifacts. In wrapper mode, artifacts live directly under
// .otelc-build/debug/<package>/. In direct mode, artifacts are scoped to the
// build session (.otelc-build/debug/sessions/<session>/<package>/) so concurrent
// independent builds do not invalidate each other's active diff reports.
func (ip *instrumentPhase) debugArtifactDir() string {
	modPath := util.FindFlagValue(ip.compileArgs, "-p")
	pkgDir := util.EscapePackagePath(modPath)
	if isWrapperMode() {
		return util.GetBuildTemp(filepath.Join("debug", pkgDir))
	}
	session := GetCurrentBuildSession()
	return DebugSessionDir(session, pkgDir)
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

	// Check if the current compile command matches the rules.
	matched := ip.match(allSet, args)
	if !matched.IsEmpty() {
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

		// Resolve package archive location, passing build flags to match the current build context
		buildFlags := util.GetBuildFlags()
		archives, err := pkgload.ResolveExportFiles(ctx, importPath, buildFlags...)
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

// CleanupImportTrackingFiles removes import tracking files from previous builds.
// Should be called at the start of a new build to clean up stale files from prior runs.
// This is exported for use by the setup phase.
func CleanupImportTrackingFiles() {
	pattern := util.GetAddedImportsPattern()
	files, err := filepath.Glob(pattern)
	if err != nil {
		return
	}

	for _, file := range files {
		_ = os.Remove(file) // Best effort cleanup
	}
}

func debugLockPath() string {
	return util.GetBuildTemp("debug.lock")
}

func debugSessionsBaseDir() string {
	return util.GetBuildTemp(filepath.Join("debug", debugSessionsDirName))
}

// DebugSessionDir returns the directory holding artifacts for a given build session,
// optionally appending subpaths (such as package directories).
func DebugSessionDir(session string, subpath ...string) string {
	elem := append([]string{"debug", debugSessionsDirName, session}, subpath...)
	return util.GetBuildTemp(filepath.Join(elem...))
}

func debugSessionReadyMarker(session string) string {
	return filepath.Join(DebugSessionDir(session), ".ready")
}

func debugLatestSessionFilePath() string {
	return filepath.Join(debugSessionsBaseDir(), "latest")
}

// RecordDebugSession writes the given session token to the latest session file.
func RecordDebugSession(token string) error {
	path := debugLatestSessionFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token), 0o600)
}

// ReadDebugSession reads the latest session token.
// If the file does not exist, it returns an empty string without error.
func ReadDebugSession() (string, error) {
	path := debugLatestSessionFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// GetCurrentBuildSession returns the session token for the current build.
// If EnvOtelcBuildSession is set (wrapper mode), it returns that value.
// Otherwise (direct mode), it derives the token from the parent process ID (ppid).
func GetCurrentBuildSession() string {
	if s := os.Getenv(util.EnvOtelcBuildSession); s != "" {
		return s
	}
	return fmt.Sprintf("direct_%d", os.Getppid())
}

// CleanStaleSessions removes session directories from previous builds whose
// parent processes are no longer alive. Active overlapping builds are preserved.
func CleanStaleSessions() error {
	sessionsDir := debugSessionsBaseDir()
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "direct_") {
			continue
		}
		pidStr := strings.TrimPrefix(name, "direct_")
		if idx := strings.IndexByte(pidStr, '_'); idx != -1 {
			pidStr = pidStr[:idx]
		}
		pid, convErr := strconv.Atoi(pidStr)
		if convErr != nil {
			continue
		}
		// If process is dead, prune this session directory
		if !util.IsProcessAlive(pid) {
			if rmErr := os.RemoveAll(filepath.Join(sessionsDir, name)); rmErr != nil && !os.IsNotExist(rmErr) {
				errs = append(errs, rmErr)
			}
		}
	}
	return errors.Join(errs...)
}

func copySetupRuntimeArtifacts(sessionDir string) error {
	debugDir := util.GetBuildTemp("debug")
	entries, err := os.ReadDir(debugDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == debugSessionsDirName {
			continue
		}
		pkgName := entry.Name()
		pkgDir := filepath.Join(debugDir, pkgName)

		runtimeSrc := filepath.Join(pkgDir, "otelc.runtime.go")
		runtimeDiff := filepath.Join(pkgDir, "otelc.runtime.go.diff")

		targetPkgDir := filepath.Join(sessionDir, pkgName)

		if _, statErr := os.Stat(runtimeSrc); statErr == nil {
			if mkErr := os.MkdirAll(targetPkgDir, 0o755); mkErr != nil {
				return mkErr
			}
			if cpErr := util.CopyFile(runtimeSrc, filepath.Join(targetPkgDir, "otelc.runtime.go")); cpErr != nil {
				return cpErr
			}
		}
		if _, statErr := os.Stat(runtimeDiff); statErr == nil {
			if mkErr := os.MkdirAll(targetPkgDir, 0o755); mkErr != nil {
				return mkErr
			}
			if cpErr := util.CopyFile(runtimeDiff, filepath.Join(targetPkgDir, "otelc.runtime.go.diff")); cpErr != nil {
				return cpErr
			}
		}
	}
	return nil
}

func cleanPackageCompilerArtifacts(entryPath string) error {
	subEntries, subErr := os.ReadDir(entryPath)
	if subErr != nil {
		return subErr
	}
	var errs []error
	remaining := 0
	for _, sub := range subEntries {
		name := sub.Name()
		if name == "otelc.runtime.go" || name == "otelc.runtime.go.diff" {
			remaining++
			continue
		}
		if rmErr := os.Remove(filepath.Join(entryPath, name)); rmErr != nil && !os.IsNotExist(rmErr) {
			errs = append(errs, rmErr)
		}
	}
	if remaining == 0 {
		if rmErr := os.Remove(entryPath); rmErr != nil && !os.IsNotExist(rmErr) {
			errs = append(errs, rmErr)
		}
	}
	return errors.Join(errs...)
}

// CleanStaleCompilerArtifacts removes compiled source diffs and retained source files
// from previous builds, while preserving runtime helper artifacts (otelc.runtime.go and
// otelc.runtime.go.diff) generated during the setup phase.
func CleanStaleCompilerArtifacts() error {
	debugDir := util.GetBuildTemp("debug")
	entries, err := os.ReadDir(debugDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var errs []error
	for _, entry := range entries {
		entryPath := filepath.Join(debugDir, entry.Name())
		if !entry.IsDir() {
			if entry.Name() != ".session" {
				if rmErr := os.Remove(entryPath); rmErr != nil && !os.IsNotExist(rmErr) {
					errs = append(errs, rmErr)
				}
			}
			continue
		}
		if entry.Name() == debugSessionsDirName {
			continue
		}

		if pkgErr := cleanPackageCompilerArtifacts(entryPath); pkgErr != nil {
			errs = append(errs, pkgErr)
		}
	}
	return errors.Join(errs...)
}

// EnsureDebugInitialized ensures that debug artifacts for the current build session
// are initialized safely and exactly once across concurrent toolexec processes.
// In direct mode, each build receives an isolated session directory so overlapping
// builds never delete each other's active diff reports.
func EnsureDebugInitialized(ctx context.Context) error {
	return EnsureDebugInitializedWithCleaner(ctx, GetCurrentBuildSession(), os.RemoveAll)
}

// EnsureDebugInitializedWithCleaner initializes the debug session directory using the provided
// cleaner function. Exported for tests requiring simulated cleanup failure injection.
func EnsureDebugInitializedWithCleaner(_ context.Context, session string, cleaner func(string) error) error {
	if !DiffDebugEnabled() {
		return nil
	}

	// In wrapper mode, runGoBuild already established and cleaned debug output upfront
	if isWrapperMode() {
		return nil
	}

	// If no otelc work directory was discovered (e.g. toolchain invocation for GOROOT
	// packages running from a directory without .otelc-build), skip debug initialization.
	if os.Getenv(util.EnvOtelcWorkDir) == "" {
		return nil
	}

	sessionDir := DebugSessionDir(session)
	readyMarker := debugSessionReadyMarker(session)

	// Fast lock-free check
	if _, err := os.Stat(readyMarker); err == nil {
		return nil
	}

	// Acquire advisory lock
	lockPath := debugLockPath()
	if mkErr := os.MkdirAll(filepath.Dir(lockPath), 0o755); mkErr != nil {
		return mkErr
	}
	fileLock := flock.New(lockPath)
	if lockErr := fileLock.Lock(); lockErr != nil {
		return ex.Wrapf(lockErr, "locking debug initialization")
	}
	defer func() {
		_ = fileLock.Unlock()
	}()

	// Double-check under lock
	if _, err := os.Stat(readyMarker); err == nil {
		return nil
	}

	// Prune dead sessions from prior finished builds (active overlapping builds are preserved)
	_ = CleanStaleSessions()

	// Clean this session directory if it previously existed (e.g. reused PID)
	if err := cleaner(sessionDir); err != nil && !os.IsNotExist(err) {
		return ex.Wrapf(err, "cleaning session directory %s", sessionDir)
	}

	// Create fresh session directory
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return ex.Wrapf(err, "creating session directory %s", sessionDir)
	}

	// Copy prerequisite setup runtime artifacts into this build's session directory
	if err := copySetupRuntimeArtifacts(sessionDir); err != nil {
		return ex.Wrapf(err, "copying setup runtime artifacts to session %s", session)
	}

	// Record latest session for discoverability
	if err := RecordDebugSession(session); err != nil {
		return ex.Wrapf(err, "recording latest debug session")
	}

	// Publish ready marker only after successful namespace establishment
	if err := os.WriteFile(readyMarker, []byte(fmt.Sprintf("%d\n", time.Now().UnixNano())), 0o600); err != nil {
		return ex.Wrapf(err, "writing debug session ready marker")
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

// loadAddedImports discovers and merges all per-process import tracking files.
func loadAddedImports(ctx context.Context) (map[string]string, error) {
	logger := util.LoggerFromContext(ctx)
	pattern := util.GetAddedImportsPattern()

	// Find all per-process import files
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, ex.Wrapf(err, "globbing import files")
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

	// Load imports that were added during compilation
	addedImports, err := loadAddedImports(ctx)
	if err != nil {
		logger.WarnContext(ctx, "failed to load added imports for link phase", "error", err)
		return args, nil // Non-fatal, proceed with original args
	}

	if len(addedImports) == 0 {
		// No imports were added during compilation
		return args, nil
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

	var goFiles []string
	if err = json.Unmarshal(config["GoFiles"], &goFiles); err != nil {
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
// another otelc spawned; such invocations only rewrite tool version probes.
func Toolexec(ctx context.Context, args []string, nested bool) error {
	// Initialize debug artifacts once per build session if debug diffs are enabled
	if DiffDebugEnabled() && !nested {
		if err := EnsureDebugInitialized(ctx); err != nil {
			return ex.Wrapf(err, "failed to initialize debug artifacts")
		}
	}

	// Use slice-based detection to correctly handle tool paths with spaces
	// (common on Windows, e.g., "C:\Program Files\Go\pkg\tool\...")

	// go derives each tool's ID (an input to every build cache key) from
	// `tool -V=full` run through toolexec. Answer it with an otelc marker
	// appended so instrumented artifacts never share cache entries with plain
	// builds; instrumentation changes compile output without changing any
	// input go hashes.
	if len(args) == 2 && args[1] == "-V=full" {
		return interceptToolVersion(ctx, args)
	}

	// The tool version rewrite above already keeps a nested build's cache keys
	// aligned with the outer one; instrumenting here too would recurse.
	if !nested {
		var err error
		args, err = interceptToolCommand(ctx, args)
		if err != nil {
			return err
		}
	}

	// Run the command
	if os.Getenv(util.EnvOtelcStats) == "" {
		return util.RunCmd(ctx, args...)
	}
	tool := filepath.Base(args[0])
	pkg := util.FindFlagValue(args, "-p")
	start := time.Now()
	err := util.RunCmd(ctx, args...)
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

// EnableNestedToolexec points GOFLAGS at this executable in nested mode, so go
// commands this process spawns (e.g. `go list -export`) run through a
// version-only otelc toolexec and share this build's cache keys. Any existing
// -toolexec was stripped at startup. Must only be called from the real otelc
// binary, since os.Executable is what nested go commands will run.
func EnableNestedToolexec() error {
	execPath, err := os.Executable()
	if err != nil {
		return ex.Wrapf(err, "resolving otelc executable path")
	}
	toolexecFlag, err := util.QuoteGoflagsToken(fmt.Sprintf("-toolexec=%s toolexec", execPath))
	if err != nil {
		return ex.Wrapf(err, "quoting nested toolexec GOFLAGS entry")
	}
	goflags := strings.TrimSpace(os.Getenv("GOFLAGS") + " " + toolexecFlag)
	if err = os.Setenv("GOFLAGS", goflags); err != nil {
		return ex.Wrapf(err, "setting GOFLAGS for nested go commands")
	}
	if err = os.Setenv(util.EnvOtelcNestedToolexec, "1"); err != nil {
		return ex.Wrapf(err, "setting %s", util.EnvOtelcNestedToolexec)
	}
	return nil
}
