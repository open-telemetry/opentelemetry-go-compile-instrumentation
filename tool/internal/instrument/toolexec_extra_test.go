// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/util"
)

// goCompileToolPath returns the path to the toolchain's compile binary, which
// answers the `-V=full` probe the same way a real toolexec sees it.
func goCompileToolPath(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOTOOLDIR").Output()
	require.NoError(t, err)
	name := "compile"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(strings.TrimSpace(string(out)), name)
}

func TestKeepForDebugCopyError(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	ip := &InstrumentPhase{
		logger:      slog.Default(),
		compileArgs: []string{"-p", "example.com/mod/pkg"},
	}
	// The source file does not exist, so CopyFile fails. The failure is only
	// logged as a warning because this is best-effort debugging output.
	ip.keepForDebug(filepath.Join(workDir, "missing.go"))
}

func TestInterceptCompileImportCfgParseError(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	args := []string{
		"compile",
		"-o", filepath.Join(t.TempDir(), "out.a"),
		"-importcfg", filepath.Join(t.TempDir(), "missing.importcfg"),
		"-p", "main",
	}
	_, err := interceptCompile(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing importcfg")
}

func TestUpdateImportConfigAddsResolvedImport(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	cfgPath := filepath.Join(workDir, "importcfg")
	require.NoError(t, os.WriteFile(cfgPath, []byte("packagefile context=/unused/context.a\n"), 0o644))

	ip := &InstrumentPhase{
		logger:           slog.Default(),
		importConfigPath: cfgPath,
		importConfig: imports.ImportConfig{
			PackageFile: map[string]string{"context": "/unused/context.a"},
		},
	}
	require.NoError(t, ip.updateImportConfig(t.Context(), map[string]string{"fmt": "fmt"}))

	content, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "packagefile fmt=")
}

func TestUpdateImportConfigResolveError(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	cfgPath := filepath.Join(workDir, "importcfg")
	require.NoError(t, os.WriteFile(cfgPath, []byte(""), 0o644))

	ip := &InstrumentPhase{
		logger:           slog.Default(),
		importConfigPath: cfgPath,
		importConfig: imports.ImportConfig{
			PackageFile: map[string]string{},
		},
	}
	// The import path does not exist, so archive resolution fails.
	err := ip.updateImportConfig(t.Context(), map[string]string{"example.invalid/nonexistent/pkg": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving")
}

func TestUpdateImportConfigWriteError(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	// The parent directory does not exist, so the importcfg rewrite fails.
	cfgPath := filepath.Join(workDir, "missing", "importcfg")
	ip := &InstrumentPhase{
		logger:           slog.Default(),
		importConfigPath: cfgPath,
		importConfig: imports.ImportConfig{
			PackageFile: map[string]string{},
		},
	}
	err := ip.updateImportConfig(t.Context(), map[string]string{"fmt": "fmt"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing importcfg")
}

func TestUpdateImportConfigTrackError(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	// No .otelc-build directory is created, so the per-process tracking file
	// write fails after the importcfg is updated. That failure is non-fatal.
	cfgPath := filepath.Join(workDir, "importcfg")
	require.NoError(t, os.WriteFile(cfgPath, []byte(""), 0o644))

	ip := &InstrumentPhase{
		logger:           slog.Default(),
		importConfigPath: cfgPath,
		importConfig: imports.ImportConfig{
			PackageFile: map[string]string{},
		},
	}
	require.NoError(t, ip.updateImportConfig(t.Context(), map[string]string{"fmt": "fmt"}))
}

func TestTrackAddedImportsWriteError(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	// No .otelc-build directory exists, so the write fails.
	err := trackAddedImports(map[string]string{"fmt": "/path/to/fmt.a"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing imports file")
}

func TestCleanupImportTrackingFilesGlobError(t *testing.T) {
	workDir := t.TempDir()
	// '[' makes the glob pattern malformed, so filepath.Glob errors and the
	// cleanup bails out.
	t.Setenv(util.EnvOtelcWorkDir, filepath.Join(workDir, "bad[glob"))
	CleanupImportTrackingFiles()
}

func TestLoadAddedImportsGlobError(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	t.Setenv(util.EnvOtelcWorkDir, filepath.Join(t.TempDir(), "bad[glob"))
	_, err := loadAddedImports(ctx)
	require.Error(t, err)
}

func TestLoadAddedImportsReadError(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	buildDir := util.GetBuildTempDir()
	require.NoError(t, os.MkdirAll(buildDir, 0o755))
	// A directory matching the tracking-file pattern cannot be read.
	require.NoError(t, os.Mkdir(filepath.Join(buildDir, "added_imports.1.json"), 0o755))

	result, err := loadAddedImports(ctx)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func writeAddedImports(t *testing.T, packages map[string]string) {
	t.Helper()
	data, err := json.Marshal(packages)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(util.GetBuildTempDir(), "added_imports.1.json"), data, 0o644))
}

func TestInterceptLinkNoImportCfg(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	args := []string{"link", "-o", "exe", "-buildid", "id"}
	result, err := interceptLink(ctx, args)
	require.NoError(t, err)
	assert.Equal(t, args, result)
}

func TestInterceptLinkLoadError(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	// A malformed glob pattern makes loadAddedImports fail; the link command
	// is then passed through unchanged.
	t.Setenv(util.EnvOtelcWorkDir, filepath.Join(t.TempDir(), "bad[glob"))

	args := []string{"link", "-o", "exe", "-buildid", "id", "-importcfg", "importcfg.link"}
	result, err := interceptLink(ctx, args)
	require.NoError(t, err)
	assert.Equal(t, args, result)
}

func TestInterceptLinkParseError(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	writeAddedImports(t, map[string]string{"fmt": "/new/fmt.a"})

	args := []string{"link", "-o", "exe", "-buildid", "id", "-importcfg", filepath.Join(workDir, "missing.link")}
	_, err := interceptLink(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing link importcfg")
}

func TestInterceptLinkUpdatesConfig(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	writeAddedImports(t, map[string]string{"fmt": "/new/fmt.a"})

	linkCfg := filepath.Join(workDir, "importcfg.link")
	require.NoError(t, os.WriteFile(linkCfg, []byte(""), 0o644))

	args := []string{"link", "-o", "exe", "-buildid", "id", "-importcfg", linkCfg}
	result, err := interceptLink(ctx, args)
	require.NoError(t, err)
	assert.Equal(t, args, result)

	content, err := os.ReadFile(linkCfg)
	require.NoError(t, err)
	assert.Contains(t, string(content), "packagefile fmt=/new/fmt.a")
}

func TestInterceptLinkAllImportsPresent(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	writeAddedImports(t, map[string]string{"fmt": "/new/fmt.a"})

	linkCfg := filepath.Join(workDir, "importcfg.link")
	require.NoError(t, os.WriteFile(linkCfg, []byte("packagefile fmt=/old/fmt.a\n"), 0o644))

	args := []string{"link", "-o", "exe", "-buildid", "id", "-importcfg", linkCfg}
	result, err := interceptLink(ctx, args)
	require.NoError(t, err)
	assert.Equal(t, args, result)

	content, err := os.ReadFile(linkCfg)
	require.NoError(t, err)
	assert.Equal(t, "packagefile fmt=/old/fmt.a\n", string(content))
}

func TestInterceptLinkWriteError(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	writeAddedImports(t, map[string]string{"fmt": "/new/fmt.a"})

	linkCfg := filepath.Join(workDir, "importcfg.link")
	require.NoError(t, os.WriteFile(linkCfg, []byte("packagefile context=/old/context.a\n"), 0o644))
	// Make the file read-only so the rewrite fails.
	require.NoError(t, os.Chmod(linkCfg, 0o444))
	t.Cleanup(func() { _ = os.Chmod(linkCfg, 0o600) })

	args := []string{"link", "-o", "exe", "-buildid", "id", "-importcfg", linkCfg}
	_, err := interceptLink(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing link importcfg")
}

func TestInterceptToolVersionWriteError(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	// Replace os.Stdout with a closed file so the tool version line write fails.
	oldStdout := os.Stdout
	f, err := os.Create(filepath.Join(t.TempDir(), "closed"))
	require.NoError(t, err)
	require.NoError(t, f.Close())
	os.Stdout = f                               //nolint:reassign // interceptToolVersion writes to os.Stdout; replace it with a closed file to force a write error
	t.Cleanup(func() { os.Stdout = oldStdout }) //nolint:reassign // restore the original os.Stdout

	err = interceptToolVersion(ctx, []string{goCompileToolPath(t), "-V=full"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing tool version")
}
