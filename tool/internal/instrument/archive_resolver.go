// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/util"
)

const (
	probeWorkDirName        = "probe_work"
	resolvedArchivesDirName = "resolved_archives"
	filePermPrivate         = 0o600
	dirPermPrivate          = 0o750
	probeBaseArgCount       = 3
)

// resolvedArchivesCache stores resolved archives across compiles and link within a build.
type resolvedArchivesCache struct {
	Compile map[string]string `json:"compile"`
	Link    map[string]string `json:"link"`
}

// archiveCacheMu serializes in-process access to the archive cache.
//
//nolint:gochecknoglobals // process-level synchronization for archive caching
var archiveCacheMu sync.Mutex

// inMemoryCache stores in-process resolved archives to avoid repeated disk reads.
//
//nolint:gochecknoglobals // process-level in-memory archive cache
var inMemoryCache = &resolvedArchivesCache{
	Compile: make(map[string]string),
	Link:    make(map[string]string),
}

// resolvePackageArchives resolves compiler and linker archives for importPaths.
//
// On Go < 1.28 (where hasExportTool is false), go list -export outputs compiler archives,
// so pkgload.ResolveExportFilesBatch serves both compile and link.
//
// From Go 1.28+, go list -export outputs tools-format export data (golang/go#79427)
// which cmd/compile and cmd/link reject. This function builds a synthetic library probe package
// importing the requested paths, using cmd/go to populate compiler and linker archives.
//
//nolint:revive // confusing-results conflicts with nonamedreturns
func resolvePackageArchives(
	ctx context.Context,
	toolPath string,
	importPaths []string,
	buildFlags []string,
) (map[string]string, map[string]string, map[string]error, error) {
	if len(importPaths) == 0 {
		return map[string]string{}, map[string]string{}, nil, nil
	}

	if toolPath == "" || !hasExportTool(toolPath) {
		archives, resFailed, resErr := pkgload.ResolveExportFilesBatch(ctx, importPaths, buildFlags...)
		if resErr != nil {
			return nil, nil, nil, resErr
		}
		return archives, maps.Clone(archives), resFailed, nil
	}

	// Go 1.28+: check in-memory and on-disk cross-process cache in .otelc-build/
	cache := loadArchiveCache()

	missing := make([]string, 0, len(importPaths))
	for _, p := range importPaths {
		cArchive, okC := cache.Compile[p]
		lArchive, okL := cache.Link[p]
		if !okC || !okL || !util.PathExists(cArchive) || !util.PathExists(lArchive) {
			missing = append(missing, p)
		}
	}

	if len(missing) == 0 {
		return cache.Compile, cache.Link, nil, nil
	}

	// Separate internal packages (which cannot be imported by arbitrary synthetic packages)
	// from standard packages that can be batched together.
	regularMissing := make([]string, 0, len(missing))
	internalMissing := make([]string, 0, len(missing))
	for _, p := range missing {
		if isInternalPackage(p) {
			internalMissing = append(internalMissing, p)
		} else {
			regularMissing = append(regularMissing, p)
		}
	}

	resolvedCompile, resolvedLink, failed := resolveRegularPackages(ctx, toolPath, regularMissing, buildFlags)

	// Resolve internal packages directly via direct build rather than synthetic imports.
	for _, p := range internalMissing {
		cArchives, lArchives, directErr := runDirectProbeBuild(ctx, toolPath, p, buildFlags)
		if directErr != nil {
			failed[p] = directErr
			continue
		}
		maps.Copy(resolvedCompile, cArchives)
		maps.Copy(resolvedLink, lArchives)
	}

	if len(resolvedCompile) > 0 {
		saveArchiveCache(resolvedCompile, resolvedLink)
		cache = loadArchiveCache()
		return cache.Compile, cache.Link, failed, nil
	}

	var firstErr error
	for _, p := range missing {
		if err, ok := failed[p]; ok {
			firstErr = err
			break
		}
	}
	return nil, nil, failed, firstErr
}

// resolveRegularPackages resolves non-internal packages via batch or fallback individual probes.
//
//nolint:revive // confusing-results conflicts with nonamedreturns
func resolveRegularPackages(
	ctx context.Context,
	toolPath string,
	missing []string,
	buildFlags []string,
) (map[string]string, map[string]string, map[string]error) {
	cArchives := make(map[string]string)
	lArchives := make(map[string]string)
	failed := make(map[string]error)
	if len(missing) == 0 {
		return cArchives, lArchives, failed
	}
	batchCompile, batchLink, batchErr := runProbeBuild(ctx, toolPath, missing, buildFlags)
	switch {
	case batchErr == nil:
		maps.Copy(cArchives, batchCompile)
		maps.Copy(lArchives, batchLink)
	case len(missing) == 1:
		failed[missing[0]] = batchErr
	default:
		for _, p := range missing {
			singleCompile, singleLink, singleErr := runProbeBuild(ctx, toolPath, []string{p}, buildFlags)
			if singleErr != nil {
				failed[p] = singleErr
				continue
			}
			maps.Copy(cArchives, singleCompile)
			maps.Copy(lArchives, singleLink)
		}
	}
	return cArchives, lArchives, failed
}

// isInternalPackage reports whether importPath is or contains an internal package element.
func isInternalPackage(importPath string) bool {
	return strings.HasPrefix(importPath, "internal/") ||
		strings.Contains(importPath, "/internal/") ||
		strings.HasSuffix(importPath, "/internal") ||
		importPath == "internal"
}

// runProbeBuild compiles a synthetic library package importing importPaths, capturing
// compiler and linker archive locations from cmd/go's importcfg files across WORK.
//
//nolint:revive // confusing-results conflicts with nonamedreturns
func runProbeBuild(
	ctx context.Context,
	toolPath string,
	importPaths []string,
	buildFlags []string,
) (map[string]string, map[string]string, error) {
	tempDir := util.GetBuildTempDir()
	probeWorkBase := filepath.Join(tempDir, probeWorkDirName)
	if mkdirErr := os.MkdirAll(probeWorkBase, dirPermPrivate); mkdirErr != nil {
		return nil, nil, ex.Wrapf(mkdirErr, "creating probe work dir %s", probeWorkBase)
	}

	probeName := fmt.Sprintf("probe-%d-%d", time.Now().UnixNano(), os.Getpid())
	probeDir := filepath.Join(tempDir, probeName)
	if mkdirErr := os.MkdirAll(probeDir, dirPermPrivate); mkdirErr != nil {
		return nil, nil, ex.Wrapf(mkdirErr, "creating probe directory %s", probeDir)
	}
	defer func() {
		_ = os.RemoveAll(probeDir)
	}()

	var src bytes.Buffer
	_, _ = src.WriteString("// Code generated by otelc archive probe. DO NOT EDIT.\n")
	_, _ = fmt.Fprintf(&src, "// nonce: %d\n", time.Now().UnixNano())
	_, _ = src.WriteString("package probe\n\nimport (\n")
	for _, p := range importPaths {
		_, _ = fmt.Fprintf(&src, "\t_ %q\n", p)
	}
	_, _ = src.WriteString(")\n")

	probeFile := filepath.Join(probeDir, "probe.go")
	if writeErr := os.WriteFile(probeFile, src.Bytes(), filePermPrivate); writeErr != nil {
		return nil, nil, ex.Wrapf(writeErr, "writing probe.go")
	}

	goBin := findGoBinary(toolPath)

	args := make([]string, 0, probeBaseArgCount+len(buildFlags))
	args = append(args, "build", "-work")
	args = append(args, buildFlags...)
	args = append(args, probeDir)

	cmd := exec.CommandContext(ctx, goBin, args...)
	cmd.Dir = util.GetOtelcWorkDir()
	cmd.Env = probeEnv(probeWorkBase)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	cmdErr := cmd.Run()
	stderrStr := stderr.String()
	workDir := parseWorkDir(stderrStr)

	if cmdErr != nil {
		return nil, nil, ex.Wrapf(cmdErr, "probe build failed for %v: %s", importPaths, strings.TrimSpace(stderrStr))
	}

	if workDir == "" {
		return nil, nil, ex.Newf("probe build succeeded but WORK directory not found in output: %s", stderrStr)
	}

	return collectArchivesFromWorkDir(workDir)
}

// runDirectProbeBuild builds a specific package directly with go build -work, capturing its
// compiler and linker archives. This avoids importing restrictions (such as internal packages).
//
//nolint:revive // confusing-results conflicts with nonamedreturns
func runDirectProbeBuild(
	ctx context.Context,
	toolPath string,
	importPath string,
	buildFlags []string,
) (map[string]string, map[string]string, error) {
	tempDir := util.GetBuildTempDir()
	probeWorkBase := filepath.Join(tempDir, probeWorkDirName)
	if mkdirErr := os.MkdirAll(probeWorkBase, dirPermPrivate); mkdirErr != nil {
		return nil, nil, ex.Wrapf(mkdirErr, "creating probe work dir %s", probeWorkBase)
	}

	goBin := findGoBinary(toolPath)

	args := make([]string, 0, probeBaseArgCount+len(buildFlags)+1)
	args = append(args, "build", "-work")
	args = append(args, buildFlags...)
	args = append(args, importPath)

	cmd := exec.CommandContext(ctx, goBin, args...)
	cmd.Dir = util.GetOtelcWorkDir()
	cmd.Env = probeEnv(probeWorkBase)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	cmdErr := cmd.Run()
	stderrStr := stderr.String()
	workDir := parseWorkDir(stderrStr)

	if cmdErr != nil {
		return nil, nil, ex.Wrapf(cmdErr, "direct build failed for %q: %s", importPath, strings.TrimSpace(stderrStr))
	}
	if workDir == "" {
		return nil, nil, ex.Newf(
			"direct build succeeded for %q but WORK directory not found: %s",
			importPath,
			stderrStr,
		)
	}

	compileArchives, linkArchives, err := collectArchivesFromWorkDir(workDir)
	if err == nil && compileArchives[importPath] != "" && linkArchives[importPath] != "" {
		return compileArchives, linkArchives, nil
	}

	// If the package was already cached, re-run with -a to force artifact creation into WORK.
	argsWithForce := make([]string, 0, len(args)+1)
	argsWithForce = append(argsWithForce, "build", "-work", "-a")
	argsWithForce = append(argsWithForce, buildFlags...)
	argsWithForce = append(argsWithForce, importPath)

	cmd = exec.CommandContext(ctx, goBin, argsWithForce...)
	cmd.Dir = util.GetOtelcWorkDir()
	cmd.Env = probeEnv(probeWorkBase)
	stderr.Reset()
	cmd.Stderr = &stderr

	if runErr := cmd.Run(); runErr != nil {
		return nil, nil, ex.Wrapf(
			runErr,
			"forced direct build failed for %q: %s",
			importPath,
			strings.TrimSpace(stderr.String()),
		)
	}
	forcedWorkDir := parseWorkDir(stderr.String())
	if forcedWorkDir == "" {
		return nil, nil, ex.Newf("forced direct build succeeded for %q but WORK directory not found", importPath)
	}

	return collectArchivesFromWorkDir(forcedWorkDir)
}

// collectArchivesFromWorkDir extracts all packagefile mappings from the probe build's WORK directory.
//
//nolint:revive // confusing-results conflicts with nonamedreturns
func collectArchivesFromWorkDir(workDir string) (map[string]string, map[string]string, error) {
	entries, readErr := os.ReadDir(workDir)
	if readErr != nil {
		return nil, nil, ex.Wrapf(readErr, "reading work dir %s", workDir)
	}

	compileArchives := make(map[string]string)
	linkArchives := make(map[string]string)

	parseActionConfigs(workDir, entries, compileArchives, linkArchives)

	b001LinkArchive := filepath.Join(workDir, "b001", "_pkg_.a")
	if len(compileArchives) == 0 && !util.PathExists(b001LinkArchive) {
		return nil, nil, ex.Newf("no importcfg found in WORK directory %s", workDir)
	}
	return compileArchives, linkArchives, nil
}

// parseActionConfigs scans each action directory for importcfg and importcfg.link.
func parseActionConfigs(
	workDir string,
	entries []os.DirEntry,
	compileArchives map[string]string,
	linkArchives map[string]string,
) {
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "b") {
			continue
		}
		actionDir := filepath.Join(workDir, entry.Name())
		linkCfgPath := filepath.Join(actionDir, "importcfg.link")
		if util.PathExists(linkCfgPath) {
			if cfg, parseErr := imports.ParseImportCfg(linkCfgPath); parseErr == nil {
				maps.Copy(linkArchives, cfg.PackageFile)
			}
		}

		cfgPath := filepath.Join(actionDir, "importcfg")
		if !util.PathExists(cfgPath) {
			continue
		}
		cfg, parseErr := imports.ParseImportCfg(cfgPath)
		if parseErr != nil {
			continue
		}
		for pkg, archive := range cfg.PackageFile {
			if _, ok := compileArchives[pkg]; !ok {
				compileArchives[pkg] = archive
			}
			if _, ok := linkArchives[pkg]; !ok {
				linkArchives[pkg] = toLinkerArchive(workDir, archive)
			}
		}
	}
}

// toLinkerArchive converts a compiler-only archive in export/bXXX.a to the linker-compatible
// archive in bXXX/_pkg_.a if it exists.
func toLinkerArchive(workDir, archive string) string {
	exportDir := filepath.Join(workDir, "export")
	if strings.HasPrefix(archive, exportDir) {
		actionID := strings.TrimSuffix(filepath.Base(archive), ".a")
		candidate := filepath.Join(workDir, actionID, "_pkg_.a")
		if util.PathExists(candidate) {
			return candidate
		}
	}
	return archive
}

// probeEnv creates the subprocess environment for probe builds.
// It sets GOTMPDIR to ensure all temporary WORK directories are created in probeWorkBase.
func probeEnv(probeWorkBase string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if strings.HasPrefix(kv, "GOTMPDIR=") {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, "GOTMPDIR="+probeWorkBase)
	return out
}

// findGoBinary locates the go binary matching the given compiler or linker toolPath,
// or falls back to "go".
func findGoBinary(toolPath string) string {
	if toolPath != "" {
		if _, err := os.Stat(toolPath); err == nil {
			ext := filepath.Ext(toolPath)
			// Standard layout: GOROOT/pkg/tool/$GOOS_$GOARCH/compile -> GOROOT/bin/go.
			goRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(toolPath))))
			candidate := filepath.Join(goRoot, "bin", "go"+ext)
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate
			}
		}
	}
	return "go"
}

// parseWorkDir extracts the WORK directory path printed by go build -work on stderr.
func parseWorkDir(stderr string) string {
	for line := range strings.SplitSeq(stderr, "\n") {
		line = strings.TrimSpace(line)
		if cut, ok := strings.CutPrefix(line, "WORK="); ok {
			return cut
		}
	}
	return ""
}

// CleanupProbeWorkDirs removes temporary probe WORK directories and disk cache records.
func CleanupProbeWorkDirs() {
	archiveCacheMu.Lock()
	inMemoryCache = &resolvedArchivesCache{
		Compile: make(map[string]string),
		Link:    make(map[string]string),
	}
	archiveCacheMu.Unlock()

	tempDir := util.GetBuildTempDir()
	_ = os.RemoveAll(filepath.Join(tempDir, probeWorkDirName))
	_ = os.RemoveAll(filepath.Join(tempDir, resolvedArchivesDirName))
}

// loadArchiveCache reads and merges all valid archive records from .otelc-build/resolved_archives/.
// It verifies that referenced artifact files still exist on disk.
func loadArchiveCache() *resolvedArchivesCache {
	archiveCacheMu.Lock()
	defer archiveCacheMu.Unlock()

	merged := &resolvedArchivesCache{
		Compile: make(map[string]string),
		Link:    make(map[string]string),
	}

	// Copy from in-memory cache first
	for pkg, path := range inMemoryCache.Compile {
		if util.PathExists(path) {
			merged.Compile[pkg] = path
		}
	}
	for pkg, path := range inMemoryCache.Link {
		if util.PathExists(path) {
			merged.Link[pkg] = path
		}
	}

	// Read on-disk records from other toolexec processes
	cacheDir := filepath.Join(util.GetBuildTempDir(), resolvedArchivesDirName)
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return merged
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		filePath := filepath.Join(cacheDir, entry.Name())
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			continue
		}

		var record resolvedArchivesCache
		if unmarshalErr := json.Unmarshal(data, &record); unmarshalErr != nil {
			continue
		}

		for pkg, cPath := range record.Compile {
			lPath, hasL := record.Link[pkg]
			if hasL && util.PathExists(cPath) && util.PathExists(lPath) {
				merged.Compile[pkg] = cPath
				merged.Link[pkg] = lPath
				inMemoryCache.Compile[pkg] = cPath
				inMemoryCache.Link[pkg] = lPath
			}
		}
	}

	return merged
}

// saveArchiveCache writes a probe-specific cache file into .otelc-build/resolved_archives/.
// Using individual atomic files avoids cross-process file-write collisions.
func saveArchiveCache(newCompile, newLink map[string]string) {
	if len(newCompile) == 0 && len(newLink) == 0 {
		return
	}

	archiveCacheMu.Lock()
	maps.Copy(inMemoryCache.Compile, newCompile)
	maps.Copy(inMemoryCache.Link, newLink)
	archiveCacheMu.Unlock()

	cacheDir := filepath.Join(util.GetBuildTempDir(), resolvedArchivesDirName)
	if err := os.MkdirAll(cacheDir, dirPermPrivate); err != nil {
		return
	}

	record := resolvedArchivesCache{
		Compile: newCompile,
		Link:    newLink,
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return
	}

	fileName := fmt.Sprintf("probe_%d_%d.json", os.Getpid(), time.Now().UnixNano())
	filePath := filepath.Join(cacheDir, fileName)
	_ = util.WriteFileAtomic(filePath, data, filePermPrivate)
}
