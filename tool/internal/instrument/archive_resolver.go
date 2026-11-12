// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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
	archiveCacheFileName  = "resolved_archives.json"
	probeWorkDirsFileName = "probe_work_dirs"
	filePermPrivate       = 0o600
	probeBaseArgCount     = 3
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

// probeWorkDirsMu serializes in-process recording of probe WORK directories.
//
//nolint:gochecknoglobals // process-level synchronization for probe WORK directory tracking
var probeWorkDirsMu sync.Mutex

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

	// Go 1.28+: check persistent cache in .otelc-build/
	archiveCacheMu.Lock()
	cache, loadErr := loadArchiveCache()
	archiveCacheMu.Unlock()

	missing := make([]string, 0, len(importPaths))
	if loadErr == nil && cache != nil {
		for _, p := range importPaths {
			if _, okC := cache.Compile[p]; !okC {
				missing = append(missing, p)
			} else if _, okL := cache.Link[p]; !okL {
				missing = append(missing, p)
			}
		}
	} else {
		missing = append(missing, importPaths...)
	}

	if len(missing) == 0 {
		return cache.Compile, cache.Link, nil, nil
	}

	// Try resolving all missing imports in a single batch probe build
	batchCompile, batchLink, batchErr := runProbeBuild(ctx, toolPath, missing, buildFlags)
	if batchErr == nil {
		archiveCacheMu.Lock()
		_ = saveArchiveCache(batchCompile, batchLink)
		cache, _ = loadArchiveCache()
		archiveCacheMu.Unlock()
		return cache.Compile, cache.Link, nil, nil
	}

	// Batch build failed (for example, a candidate has build constraints for another OS).
	// If only one package was requested, return the failure.
	if len(missing) == 1 {
		return nil, nil, map[string]error{missing[0]: batchErr}, nil
	}

	// Resolve missing imports individually so valid packages succeed even if one fails.
	resolvedCompile := make(map[string]string)
	resolvedLink := make(map[string]string)
	failed := make(map[string]error)

	for _, p := range missing {
		cArchives, lArchives, singleErr := runProbeBuild(ctx, toolPath, []string{p}, buildFlags)
		if singleErr != nil {
			failed[p] = singleErr
			continue
		}
		maps.Copy(resolvedCompile, cArchives)
		maps.Copy(resolvedLink, lArchives)
	}

	if len(resolvedCompile) > 0 {
		archiveCacheMu.Lock()
		_ = saveArchiveCache(resolvedCompile, resolvedLink)
		cache, _ = loadArchiveCache()
		archiveCacheMu.Unlock()
		return cache.Compile, cache.Link, failed, nil
	}

	return nil, nil, failed, batchErr
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
	if mkdirErr := os.MkdirAll(tempDir, 0o755); mkdirErr != nil {
		return nil, nil, ex.Wrapf(mkdirErr, "creating temp dir for archive probe")
	}

	probeName := fmt.Sprintf("probe-%d-%d", time.Now().UnixNano(), os.Getpid())
	probeDir := filepath.Join(tempDir, probeName)
	if mkdirErr := os.MkdirAll(probeDir, 0o755); mkdirErr != nil {
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

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	cmdErr := cmd.Run()
	stderrStr := stderr.String()

	workDir := parseWorkDir(stderrStr)
	if workDir != "" {
		recordProbeWorkDir(workDir)
	}

	if cmdErr != nil {
		return nil, nil, ex.Wrapf(cmdErr, "probe build failed for %v: %s", importPaths, strings.TrimSpace(stderrStr))
	}

	if workDir == "" {
		return nil, nil, ex.Newf("probe build succeeded but WORK directory not found in output: %s", stderrStr)
	}

	return collectArchivesFromWorkDir(workDir)
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

	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "b") {
			continue
		}
		cfgPath := filepath.Join(workDir, entry.Name(), "importcfg")
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

	if len(compileArchives) == 0 {
		return nil, nil, ex.Newf("no importcfg found in WORK directory %s", workDir)
	}
	return compileArchives, linkArchives, nil
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

// recordProbeWorkDir records a probe build's WORK directory so setup.Cleanup can remove it.
func recordProbeWorkDir(workDir string) {
	if workDir == "" {
		return
	}
	probeWorkDirsMu.Lock()
	defer probeWorkDirsMu.Unlock()

	tempDir := util.GetBuildTempDir()
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return
	}

	filePath := filepath.Join(tempDir, probeWorkDirsFileName)
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, filePermPrivate)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintln(f, workDir)
}

// CleanupProbeWorkDirs removes all temporary WORK directories created by probe builds.
func CleanupProbeWorkDirs() {
	probeWorkDirsMu.Lock()
	defer probeWorkDirsMu.Unlock()

	filePath := filepath.Join(util.GetBuildTempDir(), probeWorkDirsFileName)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}
	_ = os.Remove(filePath)

	for line := range strings.SplitSeq(string(data), "\n") {
		dir := strings.TrimSpace(line)
		if dir != "" {
			_ = os.RemoveAll(filepath.Clean(dir))
		}
	}
}

// loadArchiveCache reads the persisted archive cache from .otelc-build/resolved_archives.json.
func loadArchiveCache() (*resolvedArchivesCache, error) {
	filePath := filepath.Join(util.GetBuildTempDir(), archiveCacheFileName)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &resolvedArchivesCache{
				Compile: make(map[string]string),
				Link:    make(map[string]string),
			}, nil
		}
		return nil, ex.Wrapf(err, "reading archive cache %s", filePath)
	}

	var cache resolvedArchivesCache
	if unmarshalErr := json.Unmarshal(data, &cache); unmarshalErr != nil {
		return nil, ex.Wrapf(unmarshalErr, "unmarshalling archive cache %s", filePath)
	}
	if cache.Compile == nil {
		cache.Compile = make(map[string]string)
	}
	if cache.Link == nil {
		cache.Link = make(map[string]string)
	}
	return &cache, nil
}

// saveArchiveCache merges new compile and link archives into .otelc-build/resolved_archives.json.
func saveArchiveCache(newCompile, newLink map[string]string) error {
	cache, err := loadArchiveCache()
	if err != nil || cache == nil {
		cache = &resolvedArchivesCache{
			Compile: make(map[string]string),
			Link:    make(map[string]string),
		}
	}

	maps.Copy(cache.Compile, newCompile)
	maps.Copy(cache.Link, newLink)

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return ex.Wrapf(err, "marshalling archive cache")
	}

	tempDir := util.GetBuildTempDir()
	if mkdirErr := os.MkdirAll(tempDir, 0o755); mkdirErr != nil {
		return ex.Wrapf(mkdirErr, "creating temp dir for archive cache")
	}

	filePath := filepath.Join(tempDir, archiveCacheFileName)
	return util.WriteFileAtomic(filePath, data, filePermPrivate)
}
