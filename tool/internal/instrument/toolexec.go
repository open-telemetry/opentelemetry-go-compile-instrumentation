// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/imports"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

type InstrumentPhase struct {
	logger *slog.Logger
	// The context for this phase
	ctx context.Context
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
	tjumps []*TJump
}

func (ip *InstrumentPhase) Info(msg string, args ...any)  { ip.logger.Info(msg, args...) }
func (ip *InstrumentPhase) Error(msg string, args ...any) { ip.logger.Error(msg, args...) }
func (ip *InstrumentPhase) Warn(msg string, args ...any)  { ip.logger.Warn(msg, args...) }
func (ip *InstrumentPhase) Debug(msg string, args ...any) { ip.logger.Debug(msg, args...) }

// keepForDebug keeps the the file to .otel-build directory for debugging
func (ip *InstrumentPhase) keepForDebug(name string) {
	escape := func(s string) string {
		dirName := strings.ReplaceAll(s, "/", "_")
		dirName = strings.ReplaceAll(dirName, ".", "_")
		return dirName
	}
	modPath := util.FindFlagValue(ip.compileArgs, "-p")
	dest := filepath.Join("debug", escape(modPath), filepath.Base(name))
	err := util.CopyFile(name, util.GetBuildTemp(dest))
	if err != nil { // error is tolerable here as this is only for debugging
		ip.Warn("failed to save modified file", "dest", dest, "error", err)
	}
}

func stripCompleteFlag(args []string) []string {
	for i, arg := range args {
		if arg == "-complete" {
			return append(args[:i], args[i+1:]...)
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

	ip := &InstrumentPhase{
		logger:           util.LoggerFromContext(ctx),
		ctx:              ctx,
		workDir:          filepath.Dir(target),
		compileArgs:      args,
		importConfigPath: importCfgPath,
	}

	// Parse existing importcfg if present
	if importCfgPath != "" {
		imports, err := imports.ParseImportCfg(importCfgPath)
		if err != nil {
			return nil, ex.Wrapf(err, "parsing importcfg")
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
		err = ip.instrument(matched)
		if err != nil {
			return nil, err
		}

		// Strip -complete flag as we may insert some hook points that are
		// not ready yet, i.e. they don't have function body
		ip.compileArgs = stripCompleteFlag(ip.compileArgs)
		ip.Info("Run instrumented command", "args", ip.compileArgs)
	}

	return ip.compileArgs, nil
}

// updateImportConfig updates the importcfg file with new imports that were added during instrumentation.
func (ip *InstrumentPhase) updateImportConfig(newImports map[string]string) error {
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
		archives, err := imports.ResolvePackageInfo(ip.ctx, importPath, buildFlags...)
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

	// Atomic write: write to temp file first
	tempPath := ip.importConfigPath + ".tmp"
	if err := ip.importConfig.WriteFile(tempPath); err != nil {
		return ex.Wrapf(err, "writing temp importcfg")
	}

	// Backup original only if backup doesn't exist yet
	backupPath := ip.importConfigPath + ".original"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		if err = util.CopyFile(ip.importConfigPath, backupPath); err != nil {
			ip.Warn("failed to backup importcfg", "error", err)
		}
	}

	// Atomic replacement
	if util.IsWindows() {
		if err := os.Remove(ip.importConfigPath); err != nil && !os.IsNotExist(err) {
			_ = os.Remove(tempPath) // Cleanup temp file on error - failure is non-critical
			return ex.Wrapf(err, "removing old importcfg")
		}
	}
	if err := os.Rename(tempPath, ip.importConfigPath); err != nil {
		return ex.Wrapf(err, "renaming temp importcfg")
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

	// Atomic write: temp file + rename
	tempPath := filePath + ".tmp"
	if writeErr := os.WriteFile(tempPath, data, 0o600); writeErr != nil {
		return ex.Wrapf(writeErr, "writing temp imports file")
	}

	// On Windows, os.Rename fails if destination exists
	if util.IsWindows() {
		if removeErr := os.Remove(filePath); removeErr != nil && !os.IsNotExist(removeErr) {
			_ = os.Remove(tempPath) // Cleanup temp file on error
			return ex.Wrapf(removeErr, "removing old imports file")
		}
	}
	if renameErr := os.Rename(tempPath, filePath); renameErr != nil {
		return ex.Wrapf(renameErr, "finalizing imports file")
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

// loadAddedImports discovers and merges all per-process import tracking files.
func loadAddedImports() (map[string]string, error) {
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
			//nolint:sloglint // no context available
			slog.Warn(
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
			//nolint:sloglint // no context available
			slog.Warn(
				"failed to parse import file",
				"path",
				filePath,
				"error",
				unmarshalErr,
			)
			continue
		}

		// Merge into result
		for pkg, archive := range imports {
			merged[pkg] = archive
		}
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
	addedImports, err := loadAddedImports()
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
		return nil, ex.Wrapf(err, "parsing link importcfg")
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

	// Atomic write: write to temp file first
	tempPath := importCfgPath + ".tmp"
	if writeErr := linkConfig.WriteFile(tempPath); writeErr != nil {
		return nil, ex.Wrapf(writeErr, "writing temp link importcfg")
	}

	// Backup original only if backup doesn't exist yet
	backupPath := importCfgPath + ".original"
	if _, statErr := os.Stat(backupPath); os.IsNotExist(statErr) {
		if copyErr := util.CopyFile(importCfgPath, backupPath); copyErr != nil {
			logger.WarnContext(ctx, "failed to backup link importcfg", "error", copyErr)
		}
	}

	// Atomic replacement
	if util.IsWindows() {
		if removeErr := os.Remove(importCfgPath); removeErr != nil && !os.IsNotExist(removeErr) {
			_ = os.Remove(tempPath) // Cleanup temp file on error
			return nil, ex.Wrapf(removeErr, "removing old link importcfg")
		}
	}
	if renameErr := os.Rename(tempPath, importCfgPath); renameErr != nil {
		return nil, ex.Wrapf(renameErr, "renaming temp link importcfg")
	}

	logger.InfoContext(ctx, "Updated link importcfg", "path", importCfgPath, "added", len(addedImports))

	// Note: We don't clean up tracking files here because multi-link builds
	// (e.g., go build ./cmd/...) need the files available for all link steps.
	// Cleanup happens at the start of the next build via CleanupImportTrackingFiles.

	return args, nil
}

// Toolexec is the entry point of the toolexec command. It intercepts all the
// commands(link, compile, asm, etc) during build process. Our responsibility is
// to find out the compile command we are interested in and run it with the
// instrumented code, and ensure the link command has all necessary dependencies.
func Toolexec(ctx context.Context, args []string) error {
	// Use slice-based detection to correctly handle tool paths with spaces
	// (common on Windows, e.g., "C:\Program Files\Go\pkg\tool\...")

	// Intercept compile commands for instrumentation
	if util.IsCompileArgs(args) {
		var err error
		args, err = interceptCompile(ctx, args)
		if err != nil {
			return err
		}
	}

	// Intercept link commands to update importcfg with added dependencies
	if util.IsLinkArgs(args) {
		var err error
		args, err = interceptLink(ctx, args)
		if err != nil {
			return err
		}
	}

	// Run the command
	return util.RunCmd(ctx, args...)
}
