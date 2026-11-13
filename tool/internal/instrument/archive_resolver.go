// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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
	directArchivesDirName   = "direct_archives"
	filePermPrivate         = 0o600
	dirPermPrivate          = 0o750
	probeEnvMaxExtraVars    = 2
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

// inMemoryCache stores in-process resolved archives keyed by configuration hash.
//
//nolint:gochecknoglobals // process-level in-memory archive cache
var inMemoryCache = make(map[string]*resolvedArchivesCache)

// resolvePackageArchives resolves compiler and linker archives for importPaths.
//
// On Go < 1.28 (where hasExportTool is false), go list -export outputs compiler archives,
// so pkgload.ResolveExportFilesBatch serves both compile and link.
//
// From Go 1.28+, go list -export outputs tools-format export data (golang/go#79427)
// which cmd/compile and cmd/link reject. This function builds a synthetic library probe
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

	configHash := computeConfigHash(toolPath, buildFlags)

	// Go 1.28+: check configuration-scoped in-memory and on-disk cross-process cache in .otelc-build/
	cache := loadArchiveCache(configHash)

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
		cArchives, lArchives, directErr := runDirectProbeBuild(ctx, toolPath, p, buildFlags, configHash)
		if directErr != nil {
			failed[p] = directErr
			continue
		}
		maps.Copy(resolvedCompile, cArchives)
		maps.Copy(resolvedLink, lArchives)
	}

	// Verify that each requested package resolved both compiler and linker archives.
	for _, p := range missing {
		if failed[p] != nil {
			continue
		}
		cPath := resolvedCompile[p]
		lPath := resolvedLink[p]
		if cPath == "" || lPath == "" || !util.PathExists(cPath) || !util.PathExists(lPath) {
			failed[p] = ex.Newf(
				"missing or invalid archive for %q (compile=%q, link=%q)",
				p,
				cPath,
				lPath,
			)
			delete(resolvedCompile, p)
			delete(resolvedLink, p)
		}
	}

	if len(resolvedCompile) > 0 {
		if saveErr := saveArchiveCache(configHash, resolvedCompile, resolvedLink); saveErr != nil {
			util.LoggerFromContext(ctx).WarnContext(
				ctx,
				"failed to persist archive cache",
				"config", configHash,
				"error", saveErr,
			)
		}
		cache = loadArchiveCache(configHash)
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

// writeHashField writes an unambiguous, length-prefixed key-value pair to h.
func writeHashField(h io.Writer, key, value string) {
	_, _ = fmt.Fprintf(h, "%d:%s=%d:%s\x00", len(key), key, len(value), value)
}

// computeConfigHash computes a deterministic fingerprint for the build configuration.
// Build flag ordering is preserved because flag semantics can be order-dependent.
func computeConfigHash(toolPath string, buildFlags []string) string {
	h := sha256.New()

	goBin, _ := findGoBinary(toolPath)
	writeHashField(h, "gobin", goBin)
	if fi, err := os.Stat(goBin); err == nil {
		writeHashField(h, "gobin_stat", fmt.Sprintf("%d:%d", fi.Size(), fi.ModTime().UnixNano()))
	}

	if toolPath != "" {
		// toolPath may be cmd/compile during compilation or cmd/link during linking.
		// Use the containing toolchain directory so compiler and linker share the same cache identity.
		writeHashField(h, "tooldir", filepath.Dir(toolPath))
	}

	goos := os.Getenv("GOOS")
	if goos == "" {
		goos = runtime.GOOS
	}
	writeHashField(h, "goos", goos)

	goarch := os.Getenv("GOARCH")
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	writeHashField(h, "goarch", goarch)

	for _, archVar := range []string{
		"GOAMD64", "GOARM", "GOARM64", "GOMIPS", "GOMIPS64",
		"GOPPC64", "GORISCV64", "GOWASM",
	} {
		if val := os.Getenv(archVar); val != "" {
			writeHashField(h, archVar, val)
		}
	}

	writeHashField(h, "cgo", os.Getenv("CGO_ENABLED"))
	writeHashField(h, "experiment", os.Getenv("GOEXPERIMENT"))
	writeHashField(h, "goflags", os.Getenv("GOFLAGS"))
	if goroot := os.Getenv("GOROOT"); goroot != "" {
		writeHashField(h, "goroot", goroot)
	}

	for i, f := range buildFlags {
		writeHashField(h, fmt.Sprintf("flag_%d", i), f)
		if cut, ok := strings.CutPrefix(f, "-pgo="); ok && cut != "off" && cut != "auto" {
			if fi, err := os.Stat(cut); err == nil {
				writeHashField(h, "pgo_file_stat", fmt.Sprintf("%d:%d", fi.Size(), fi.ModTime().UnixNano()))
			}
		}
	}

	pgoProfileFile := util.GetPGOProfileFile()
	if fi, err := os.Stat(pgoProfileFile); err == nil {
		writeHashField(h, "pgo_profile_stat", fmt.Sprintf("%d:%d", fi.Size(), fi.ModTime().UnixNano()))
	}

	return hex.EncodeToString(h.Sum(nil))[:32]
}

// extractDirFlag separates directory flags (-C / --C) from other build flags.
// In cmd/go, -C must be the first flag on the command line immediately after
// the subcommand (e.g. go build -C <dir> ...). Both "-C <dir>" and "-C=<dir>"
// (and their "--C" variants) are extracted.
//
//nolint:revive // confusing-results conflicts with nonamedreturns
func extractDirFlag(flags []string) ([]string, []string) {
	var dirFlags []string
	var otherFlags []string
	for i := 0; i < len(flags); i++ {
		f := flags[i]
		if f == "-C" || f == "--C" {
			dirFlags = append(dirFlags, f)
			if i+1 < len(flags) {
				i++
				dirFlags = append(dirFlags, flags[i])
			}
			continue
		}
		if strings.HasPrefix(f, "-C=") || strings.HasPrefix(f, "--C=") {
			dirFlags = append(dirFlags, f)
			continue
		}
		otherFlags = append(otherFlags, f)
	}
	return dirFlags, otherFlags
}

// buildProbeArgs constructs arguments for a probe "go build" command.
// It ensures any -C flags appear immediately after "build" as required by cmd/go,
// followed by probe-specific extraFlags (e.g. -work, -o <archive>),
// user build flags, and the build target.
func buildProbeArgs(buildFlags, extraFlags []string, target string) []string {
	dirFlags, otherFlags := extractDirFlag(buildFlags)
	args := make([]string, 0, 1+len(dirFlags)+len(extraFlags)+len(otherFlags)+1)
	args = append(args, "build")
	args = append(args, dirFlags...)
	args = append(args, extraFlags...)
	args = append(args, otherFlags...)
	args = append(args, target)
	return args
}

// evalSymlinks resolves symbolic links in path. If path does not exist,
// it attempts to resolve symlinks in the parent directory.
func evalSymlinks(path string) string {
	if realPath, err := filepath.EvalSymlinks(path); err == nil {
		return realPath
	}
	parent := filepath.Dir(path)
	if realParent, err := filepath.EvalSymlinks(parent); err == nil {
		return filepath.Join(realParent, filepath.Base(path))
	}
	return filepath.Clean(path)
}

// probeEffectiveDir determines the working directory for a probe build when -C flags are present.
func probeEffectiveDir(workDir string, buildFlags []string) string {
	dirFlags, _ := extractDirFlag(buildFlags)
	if len(dirFlags) == 0 {
		return workDir
	}
	lastFlag := dirFlags[len(dirFlags)-1]
	var customDir string
	if _, val, found := strings.Cut(lastFlag, "="); found {
		customDir = val
	} else {
		customDir = lastFlag
	}
	if filepath.IsAbs(customDir) {
		return customDir
	}
	return filepath.Join(workDir, customDir)
}

// resolveProbeTarget returns the package target argument for a probe build.
// When probeDir is located within the build working directory (such as in .otelc-build),
// it returns a relative path (e.g. ./.otelc-build/probe-...) rather than an absolute path.
// This prevents cmd/go from rejecting the target with "directory outside main module"
// on operating systems like macOS where temporary directories are symlinks (/var -> /private/var)
// and getcwd() canonicalizes the working directory while command-line arguments retain symlinks.
func resolveProbeTarget(buildFlags []string, probeDir string) string {
	workDir := util.GetOtelcWorkDir()
	effectiveDir := probeEffectiveDir(workDir, buildFlags)

	realWorkDir := evalSymlinks(workDir)
	realEffectiveDir := evalSymlinks(effectiveDir)
	realProbeDir := evalSymlinks(probeDir)

	if realProbeDir == realWorkDir || strings.HasPrefix(realProbeDir, realWorkDir+string(filepath.Separator)) {
		if rel, err := filepath.Rel(realEffectiveDir, realProbeDir); err == nil {
			if !strings.HasPrefix(rel, "."+string(filepath.Separator)) && !strings.HasPrefix(rel, "..") {
				return "." + string(filepath.Separator) + rel
			}
			return rel
		}
	}

	return probeDir
}

// runProbeCmd executes a go command with probe environment and returns the WORK directory.
func runProbeCmd(
	ctx context.Context,
	toolPath string,
	args []string,
	gocacheDir string,
) (string, error) {
	goBin, err := findGoBinary(toolPath)
	if err != nil {
		return "", err
	}

	tempDir := util.GetBuildTempDir()
	probeWorkBase := filepath.Join(tempDir, probeWorkDirName)
	if mkdirErr := os.MkdirAll(probeWorkBase, dirPermPrivate); mkdirErr != nil {
		return "", ex.Wrapf(mkdirErr, "creating probe work dir %s", probeWorkBase)
	}

	cmd := exec.CommandContext(ctx, goBin, args...)
	cmd.Dir = evalSymlinks(util.GetOtelcWorkDir())
	cmd.Env = probeEnv(probeWorkBase, gocacheDir)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	cmdErr := cmd.Run()
	stderrStr := stderr.String()
	workDirOut := parseWorkDir(stderrStr)

	if cmdErr != nil {
		return "", ex.Wrapf(cmdErr, "probe command failed: %s", strings.TrimSpace(stderrStr))
	}
	if workDirOut == "" {
		return "", ex.Newf("probe command succeeded but WORK directory not found: %s", stderrStr)
	}
	return workDirOut, nil
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
	probeName := fmt.Sprintf("probe-%d-%d", os.Getpid(), time.Now().UnixNano())
	probeDir := filepath.Join(tempDir, probeName)
	if mkdirErr := os.MkdirAll(probeDir, dirPermPrivate); mkdirErr != nil {
		return nil, nil, ex.Wrapf(mkdirErr, "creating probe directory %s", probeDir)
	}
	defer func() {
		_ = os.RemoveAll(probeDir)
	}()

	var src bytes.Buffer
	_, _ = src.WriteString("// Code generated by otelc archive probe. DO NOT EDIT.\n")
	_, _ = src.WriteString("package probe\n\nimport (\n")
	for _, p := range importPaths {
		_, _ = fmt.Fprintf(&src, "\t_ %q\n", p)
	}
	_, _ = src.WriteString(")\n")

	probeFile := filepath.Join(probeDir, "probe.go")
	if writeErr := os.WriteFile(probeFile, src.Bytes(), filePermPrivate); writeErr != nil {
		return nil, nil, ex.Wrapf(writeErr, "writing probe.go")
	}

	probeTarget := resolveProbeTarget(buildFlags, probeDir)
	args := buildProbeArgs(buildFlags, []string{"-work"}, probeTarget)

	workDir, cmdErr := runProbeCmd(ctx, toolPath, args, "")
	if cmdErr != nil {
		return nil, nil, ex.Wrapf(cmdErr, "probe build failed for %v", importPaths)
	}

	return collectArchivesFromWorkDir(workDir)
}

// runDirectProbeBuild builds a specific package directly with go build -work -o, capturing its
// actual compiler and linker archive without importing restrictions. Each probe produces a uniquely
// named artifact under .otelc-build/direct_archives/<configHash>/ to prevent concurrent output collisions.
// An ephemeral GOCACHE ensures that cmd/go compiles the action tree into WORK and populates all
// transitive linker dependencies even when the host build cache is warm.
//
//nolint:revive // confusing-results conflicts with nonamedreturns
func runDirectProbeBuild(
	ctx context.Context,
	toolPath string,
	importPath string,
	buildFlags []string,
	configHash string,
) (map[string]string, map[string]string, error) {
	tempDir := util.GetBuildTempDir()
	directArchivesConfigDir := filepath.Join(tempDir, directArchivesDirName, configHash)
	if mkdirErr := os.MkdirAll(directArchivesConfigDir, dirPermPrivate); mkdirErr != nil {
		return nil, nil, ex.Wrapf(mkdirErr, "creating direct archives dir %s", directArchivesConfigDir)
	}

	safePkg := util.EscapePackagePath(importPath)
	h := sha256.Sum256([]byte(importPath))
	pkgHash := hex.EncodeToString(h[:])[:12]
	outFileName := fmt.Sprintf("%s-%s-%d-%d.a", safePkg, pkgHash, os.Getpid(), time.Now().UnixNano())
	outArchive := filepath.Join(directArchivesConfigDir, outFileName)

	probeWorkBase := filepath.Join(tempDir, probeWorkDirName)
	probeGocache := filepath.Join(probeWorkBase, fmt.Sprintf("gocache-%d-%d", os.Getpid(), time.Now().UnixNano()))
	if mkdirErr := os.MkdirAll(probeGocache, dirPermPrivate); mkdirErr != nil {
		return nil, nil, ex.Wrapf(mkdirErr, "creating direct probe gocache dir %s", probeGocache)
	}
	defer func() {
		_ = os.RemoveAll(probeGocache)
	}()

	args := buildProbeArgs(buildFlags, []string{"-work", "-o", outArchive}, importPath)

	workDir, cmdErr := runProbeCmd(ctx, toolPath, args, probeGocache)
	if cmdErr != nil {
		return nil, nil, ex.Wrapf(cmdErr, "direct build failed for %q", importPath)
	}

	if !util.PathExists(outArchive) {
		return nil, nil, ex.Newf(
			"direct build succeeded for %q but output archive was not created at %s",
			importPath,
			outArchive,
		)
	}

	compileArchives := map[string]string{importPath: outArchive}
	linkArchives := map[string]string{importPath: outArchive}

	depCompile, depLink, depErr := collectArchivesFromWorkDir(workDir)
	if depErr != nil {
		return nil, nil, ex.Wrapf(depErr, "collecting dependencies for %q", importPath)
	}
	maps.Copy(compileArchives, depCompile)
	maps.Copy(linkArchives, depLink)
	compileArchives[importPath] = outArchive
	linkArchives[importPath] = outArchive

	return compileArchives, linkArchives, nil
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

	foundConfig := parseActionConfigs(workDir, entries, compileArchives, linkArchives)

	b001LinkArchive := filepath.Join(workDir, "b001", "_pkg_.a")
	if !foundConfig && len(compileArchives) == 0 && !util.PathExists(b001LinkArchive) {
		return nil, nil, ex.Newf("no importcfg found in WORK directory %s", workDir)
	}
	return compileArchives, linkArchives, nil
}

// parseActionConfigs scans each action directory for importcfg and importcfg.link.
// Action directories are processed in sorted order (b001 first) so root probe mappings take precedence.
// It reports whether at least one action config was found.
func parseActionConfigs(
	workDir string,
	entries []os.DirEntry,
	compileArchives map[string]string,
	linkArchives map[string]string,
) bool {
	actionDirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "b") {
			actionDirs = append(actionDirs, entry.Name())
		}
	}
	slices.Sort(actionDirs)
	if idx := slices.Index(actionDirs, "b001"); idx > 0 {
		actionDirs = append([]string{"b001"}, append(actionDirs[:idx], actionDirs[idx+1:]...)...)
	}

	var found bool
	for _, name := range actionDirs {
		if parseActionDir(filepath.Join(workDir, name), workDir, compileArchives, linkArchives) {
			found = true
		}
	}
	return found
}

// parseActionDir extracts compiler and linker archive mappings from a single action directory.
// It reports whether an importcfg or importcfg.link file was present.
func parseActionDir(
	actionDir string,
	workDir string,
	compileArchives map[string]string,
	linkArchives map[string]string,
) bool {
	var found bool
	linkCfgPath := filepath.Join(actionDir, "importcfg.link")
	if util.PathExists(linkCfgPath) {
		found = true
		if cfg, parseErr := imports.ParseImportCfg(linkCfgPath); parseErr == nil {
			for pkg, lArchive := range cfg.PackageFile {
				if _, ok := linkArchives[pkg]; !ok {
					linkArchives[pkg] = lArchive
				}
			}
		}
	}

	cfgPath := filepath.Join(actionDir, "importcfg")
	if !util.PathExists(cfgPath) {
		return found
	}
	found = true
	cfg, parseErr := imports.ParseImportCfg(cfgPath)
	if parseErr != nil {
		return found
	}
	for pkg, archive := range cfg.PackageFile {
		if _, ok := compileArchives[pkg]; !ok {
			compileArchives[pkg] = archive
		}
		if _, ok := linkArchives[pkg]; !ok {
			if lArchive := toLinkerArchive(workDir, archive); lArchive != "" {
				linkArchives[pkg] = lArchive
			}
		}
	}
	return found
}

// toLinkerArchive converts a compiler archive to a linker-compatible archive.
// For compiler-only export files in export/bXXX.a, it returns the object archive
// in bXXX/_pkg_.a if it exists. If the object archive does not exist, it returns
// empty string to prevent compiler-only artifacts from reaching the linker.
func toLinkerArchive(workDir, archive string) string {
	exportDir := filepath.Join(workDir, "export")
	if archive == exportDir || strings.HasPrefix(archive, exportDir+string(filepath.Separator)) {
		actionID := strings.TrimSuffix(filepath.Base(archive), ".a")
		candidate := filepath.Join(workDir, actionID, "_pkg_.a")
		if util.PathExists(candidate) {
			return candidate
		}
		// Compiler export file cannot be used as linker archive.
		return ""
	}
	return archive
}

// probeEnv creates the subprocess environment for probe builds.
// It sets GOTMPDIR to ensure all temporary WORK directories are created in probeWorkBase,
// and optionally overrides GOCACHE when an isolated cache is needed.
func probeEnv(probeWorkBase, gocacheDir string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+probeEnvMaxExtraVars)
	for _, kv := range env {
		if strings.HasPrefix(kv, "GOTMPDIR=") {
			continue
		}
		if gocacheDir != "" && strings.HasPrefix(kv, "GOCACHE=") {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, "GOTMPDIR="+probeWorkBase)
	if gocacheDir != "" {
		out = append(out, "GOCACHE="+gocacheDir)
	}
	return out
}

// resolveGoBinaryFromTool resolves the go executable matching the toolchain of toolPath.
func resolveGoBinaryFromTool(toolPath string) (string, error) {
	if _, err := os.Stat(toolPath); err != nil {
		return "", ex.Wrapf(err, "inspecting tool path %s", toolPath)
	}
	ext := filepath.Ext(toolPath)
	// Standard layout: GOROOT/pkg/tool/$GOOS_$GOARCH/compile -> GOROOT/bin/go.
	goRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(toolPath))))
	candidate := filepath.Join(goRoot, "bin", "go"+ext)
	if _, statErr := os.Stat(candidate); statErr == nil {
		return candidate, nil
	}
	if goroot := os.Getenv("GOROOT"); goroot != "" {
		cleaned := filepath.Clean(goroot)
		candidate = filepath.Join(cleaned, "bin", "go"+ext)
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, nil
		}
	}
	return "", ex.Newf("cannot locate matching go binary for tool %s", toolPath)
}

// findGoBinary locates the go binary matching the given compiler or linker toolPath,
// or resolves "go" from PATH when toolPath is empty.
func findGoBinary(toolPath string) (string, error) {
	if toolPath != "" {
		return resolveGoBinaryFromTool(toolPath)
	}
	path, err := exec.LookPath("go")
	if err != nil {
		return "", ex.Wrapf(err, "looking up go binary in PATH")
	}
	return path, nil
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

// CleanupProbeWorkDirs removes temporary probe WORK directories, direct archives, and disk cache records.
func CleanupProbeWorkDirs() {
	archiveCacheMu.Lock()
	inMemoryCache = make(map[string]*resolvedArchivesCache)
	archiveCacheMu.Unlock()

	tempDir := util.GetBuildTempDir()
	_ = os.RemoveAll(filepath.Join(tempDir, probeWorkDirName))
	_ = os.RemoveAll(filepath.Join(tempDir, resolvedArchivesDirName))
	_ = os.RemoveAll(filepath.Join(tempDir, directArchivesDirName))
}

// loadArchiveCache reads and merges all valid archive records from .otelc-build/resolved_archives/<configHash>.
// It verifies that referenced artifact files still exist on disk.
func loadArchiveCache(configHash string) *resolvedArchivesCache {
	archiveCacheMu.Lock()
	defer archiveCacheMu.Unlock()

	merged := &resolvedArchivesCache{
		Compile: make(map[string]string),
		Link:    make(map[string]string),
	}

	// Copy from in-memory cache first
	if mem, ok := inMemoryCache[configHash]; ok {
		for pkg, path := range mem.Compile {
			if util.PathExists(path) {
				merged.Compile[pkg] = path
			}
		}
		for pkg, path := range mem.Link {
			if util.PathExists(path) {
				merged.Link[pkg] = path
			}
		}
	}

	if _, ok := inMemoryCache[configHash]; !ok {
		inMemoryCache[configHash] = &resolvedArchivesCache{
			Compile: make(map[string]string),
			Link:    make(map[string]string),
		}
	}
	mem := inMemoryCache[configHash]

	// Read on-disk records from other toolexec processes for this configuration
	cacheDir := filepath.Join(util.GetBuildTempDir(), resolvedArchivesDirName, configHash)
	loadDiskArchiveRecords(cacheDir, merged, mem)

	return merged
}

// loadDiskArchiveRecords reads valid on-disk archive records from cacheDir and merges them into merged and mem.
func loadDiskArchiveRecords(cacheDir string, merged, mem *resolvedArchivesCache) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(cacheDir, entry.Name()))
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
				if _, ok := merged.Compile[pkg]; !ok {
					merged.Compile[pkg] = cPath
					merged.Link[pkg] = lPath
				}
				if _, ok := mem.Compile[pkg]; !ok {
					mem.Compile[pkg] = cPath
					mem.Link[pkg] = lPath
				}
			}
		}
	}
}

// saveArchiveCache writes a probe-specific cache file into .otelc-build/resolved_archives/<configHash>/.
// Using individual atomic files avoids cross-process file-write collisions.
func saveArchiveCache(configHash string, newCompile, newLink map[string]string) error {
	if len(newCompile) == 0 && len(newLink) == 0 {
		return nil
	}

	archiveCacheMu.Lock()
	if _, ok := inMemoryCache[configHash]; !ok {
		inMemoryCache[configHash] = &resolvedArchivesCache{
			Compile: make(map[string]string),
			Link:    make(map[string]string),
		}
	}
	mem := inMemoryCache[configHash]
	maps.Copy(mem.Compile, newCompile)
	maps.Copy(mem.Link, newLink)
	archiveCacheMu.Unlock()

	cacheDir := filepath.Join(util.GetBuildTempDir(), resolvedArchivesDirName, configHash)
	if err := os.MkdirAll(cacheDir, dirPermPrivate); err != nil {
		return ex.Wrapf(err, "creating archive cache dir %s", cacheDir)
	}

	record := resolvedArchivesCache{
		Compile: newCompile,
		Link:    newLink,
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return ex.Wrapf(err, "marshaling archive cache record")
	}

	fileName := fmt.Sprintf("probe_%d_%d.json", os.Getpid(), time.Now().UnixNano())
	filePath := filepath.Join(cacheDir, fileName)
	if writeErr := util.WriteFileAtomic(filePath, data, filePermPrivate); writeErr != nil {
		return ex.Wrapf(writeErr, "writing archive cache file %s", filePath)
	}
	return nil
}
