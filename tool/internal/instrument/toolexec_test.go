// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestStripCompleteFlag(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "empty args",
			args:     []string{},
			expected: []string{},
		},
		{
			name:     "no complete flag",
			args:     []string{"-o", "output.a", "-p", "main", "file.go"},
			expected: []string{"-o", "output.a", "-p", "main", "file.go"},
		},
		{
			name:     "complete flag at beginning",
			args:     []string{"-complete", "-o", "output.a", "-p", "main"},
			expected: []string{"-o", "output.a", "-p", "main"},
		},
		{
			name:     "complete flag in middle",
			args:     []string{"-o", "output.a", "-complete", "-p", "main"},
			expected: []string{"-o", "output.a", "-p", "main"},
		},
		{
			name:     "complete flag at end",
			args:     []string{"-o", "output.a", "-p", "main", "-complete"},
			expected: []string{"-o", "output.a", "-p", "main"},
		},
		{
			name:     "only complete flag",
			args:     []string{"-complete"},
			expected: []string{},
		},
		{
			name:     "complete as value not flag",
			args:     []string{"-mode", "-complete", "-o", "output.a"},
			expected: []string{"-mode", "-o", "output.a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy of original args to verify they are not mutated
			origArgs := make([]string, len(tt.args))
			copy(origArgs, tt.args)

			result := stripCompleteFlag(tt.args)
			assert.Equal(t, tt.expected, result)
			assert.Equal(t, origArgs, tt.args, "original slice should not be mutated")
		})
	}
}

func TestUpdateImportConfig(t *testing.T) {
	t.Run("no importcfg path", func(t *testing.T) {
		ip := &InstrumentPhase{
			importConfigPath: "",
		}
		err := ip.updateImportConfig(t.Context(), map[string]string{"fmt": "fmt"})
		require.NoError(t, err)
	})

	t.Run("empty new imports", func(t *testing.T) {
		tempDir := t.TempDir()
		cfgPath := filepath.Join(tempDir, "importcfg")
		err := os.WriteFile(cfgPath, []byte("packagefile fmt=/path/to/fmt.a\n"), 0o644)
		require.NoError(t, err)

		ip := &InstrumentPhase{
			importConfigPath: cfgPath,
			importConfig: imports.ImportConfig{
				PackageFile: map[string]string{"fmt": "/path/to/fmt.a"},
			},
		}
		err = ip.updateImportConfig(t.Context(), map[string]string{})
		require.NoError(t, err)

		// File should not be modified
		content, err := os.ReadFile(cfgPath)
		require.NoError(t, err)
		assert.Equal(t, "packagefile fmt=/path/to/fmt.a\n", string(content))
	})

	t.Run("unsafe import is skipped", func(t *testing.T) {
		tempDir := t.TempDir()
		cfgPath := filepath.Join(tempDir, "importcfg")
		err := os.WriteFile(cfgPath, []byte("packagefile fmt=/path/to/fmt.a\n"), 0o644)
		require.NoError(t, err)

		ip := &InstrumentPhase{
			importConfigPath: cfgPath,
			importConfig: imports.ImportConfig{
				PackageFile: map[string]string{"fmt": "/path/to/fmt.a"},
			},
		}
		err = ip.updateImportConfig(t.Context(), map[string]string{"unsafe": "unsafe"})
		require.NoError(t, err)

		// File should not be modified since unsafe is skipped
		content, err := os.ReadFile(cfgPath)
		require.NoError(t, err)
		assert.Equal(t, "packagefile fmt=/path/to/fmt.a\n", string(content))
	})

	t.Run("cgo C pseudo-package is skipped", func(t *testing.T) {
		tempDir := t.TempDir()
		cfgPath := filepath.Join(tempDir, "importcfg")
		err := os.WriteFile(cfgPath, []byte("packagefile fmt=/path/to/fmt.a\n"), 0o644)
		require.NoError(t, err)

		ip := &InstrumentPhase{
			importConfigPath: cfgPath,
			importConfig: imports.ImportConfig{
				PackageFile: map[string]string{"fmt": "/path/to/fmt.a"},
			},
		}
		err = ip.updateImportConfig(t.Context(), map[string]string{"C": "C"})
		require.NoError(t, err)

		// File should not be modified since C is the cgo pseudo-package
		content, err := os.ReadFile(cfgPath)
		require.NoError(t, err)
		assert.Equal(t, "packagefile fmt=/path/to/fmt.a\n", string(content))
	})

	t.Run("import already exists", func(t *testing.T) {
		tempDir := t.TempDir()
		cfgPath := filepath.Join(tempDir, "importcfg")
		err := os.WriteFile(cfgPath, []byte("packagefile fmt=/path/to/fmt.a\n"), 0o644)
		require.NoError(t, err)

		ip := &InstrumentPhase{
			importConfigPath: cfgPath,
			importConfig: imports.ImportConfig{
				PackageFile: map[string]string{"fmt": "/path/to/fmt.a"},
			},
		}
		err = ip.updateImportConfig(t.Context(), map[string]string{"fmt": "fmt"})
		require.NoError(t, err)

		// File should not be modified since fmt already exists
		content, err := os.ReadFile(cfgPath)
		require.NoError(t, err)
		assert.Equal(t, "packagefile fmt=/path/to/fmt.a\n", string(content))
	})

	t.Run("nil PackageFile map", func(t *testing.T) {
		tempDir := t.TempDir()
		cfgPath := filepath.Join(tempDir, "importcfg")
		err := os.WriteFile(cfgPath, []byte(""), 0o644)
		require.NoError(t, err)

		ip := &InstrumentPhase{
			logger:           slog.Default(),
			importConfigPath: cfgPath,
			importConfig: imports.ImportConfig{
				PackageFile: nil, // Intentionally nil
			},
		}

		// Should not panic, even though we're trying to add imports
		err = ip.updateImportConfig(t.Context(), map[string]string{"unsafe": "unsafe"})
		require.NoError(t, err)
	})
}

func TestTrackAddedImports(t *testing.T) {
	t.Run("empty packages does nothing", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, tempDir)

		err := trackAddedImports(map[string]string{})
		require.NoError(t, err)

		// No file should be created
		pattern := util.GetAddedImportsPattern()
		files, _ := filepath.Glob(pattern)
		assert.Empty(t, files)
	})

	t.Run("creates per-process file", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, tempDir)

		// Create build temp directory
		err := os.MkdirAll(util.GetBuildTempDir(), 0o755)
		require.NoError(t, err)

		packages := map[string]string{
			"fmt":     "/path/to/fmt.a",
			"context": "/path/to/context.a",
		}

		err = trackAddedImports(packages)
		require.NoError(t, err)

		// Verify file was created with correct name pattern
		expectedPath := util.GetAddedImportsFileForProcess()
		_, err = os.Stat(expectedPath)
		require.NoError(t, err, "per-process file should exist")

		// Verify contents
		data, err := os.ReadFile(expectedPath)
		require.NoError(t, err)

		var result map[string]string
		err = json.Unmarshal(data, &result)
		require.NoError(t, err)

		assert.Equal(t, packages, result)
	})
}

func TestLoadAddedImports(t *testing.T) {
	t.Run("no files returns empty map", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, tempDir)

		// Create build temp directory (empty)
		err := os.MkdirAll(util.GetBuildTempDir(), 0o755)
		require.NoError(t, err)

		result, err := loadAddedImports(t.Context())
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("merges multiple per-process files", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, tempDir)

		// Create build temp directory
		buildDir := util.GetBuildTempDir()
		err := os.MkdirAll(buildDir, 0o755)
		require.NoError(t, err)

		// Simulate files from different processes
		file1 := filepath.Join(buildDir, "added_imports.1234.json")
		file2 := filepath.Join(buildDir, "added_imports.5678.json")
		file3 := filepath.Join(buildDir, "added_imports.9012.json")

		data1, _ := json.Marshal(map[string]string{"fmt": "/path/to/fmt.a"})
		data2, _ := json.Marshal(map[string]string{"context": "/path/to/context.a"})
		data3, _ := json.Marshal(map[string]string{"strings": "/path/to/strings.a"})

		require.NoError(t, os.WriteFile(file1, data1, 0o644))
		require.NoError(t, os.WriteFile(file2, data2, 0o644))
		require.NoError(t, os.WriteFile(file3, data3, 0o644))

		result, err := loadAddedImports(t.Context())
		require.NoError(t, err)

		expected := map[string]string{
			"fmt":     "/path/to/fmt.a",
			"context": "/path/to/context.a",
			"strings": "/path/to/strings.a",
		}
		assert.Equal(t, expected, result)
	})

	t.Run("handles corrupted JSON gracefully", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, tempDir)

		// Create build temp directory
		buildDir := util.GetBuildTempDir()
		err := os.MkdirAll(buildDir, 0o755)
		require.NoError(t, err)

		// One valid file, one corrupted
		validFile := filepath.Join(buildDir, "added_imports.1111.json")
		corruptedFile := filepath.Join(buildDir, "added_imports.2222.json")

		validData, _ := json.Marshal(map[string]string{"fmt": "/path/to/fmt.a"})
		require.NoError(t, os.WriteFile(validFile, validData, 0o644))
		require.NoError(t, os.WriteFile(corruptedFile, []byte("not valid json"), 0o644))

		result, err := loadAddedImports(t.Context())
		require.NoError(t, err)

		// Should still get the valid import
		expected := map[string]string{"fmt": "/path/to/fmt.a"}
		assert.Equal(t, expected, result)
	})

	t.Run("later file overrides earlier for same package", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, tempDir)

		// Create build temp directory
		buildDir := util.GetBuildTempDir()
		err := os.MkdirAll(buildDir, 0o755)
		require.NoError(t, err)

		// Two files with same package but different archives
		// Note: filepath.Glob returns files in lexical order
		file1 := filepath.Join(buildDir, "added_imports.1111.json")
		file2 := filepath.Join(buildDir, "added_imports.2222.json")

		data1, _ := json.Marshal(map[string]string{"fmt": "/old/path/fmt.a"})
		data2, _ := json.Marshal(map[string]string{"fmt": "/new/path/fmt.a"})

		require.NoError(t, os.WriteFile(file1, data1, 0o644))
		require.NoError(t, os.WriteFile(file2, data2, 0o644))

		result, err := loadAddedImports(t.Context())
		require.NoError(t, err)

		// The second file (lexically) should win
		assert.Equal(t, "/new/path/fmt.a", result["fmt"])
	})
}

func TestCleanupImportTrackingFiles(t *testing.T) {
	t.Run("removes all tracking files", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, tempDir)

		// Create build temp directory
		buildDir := util.GetBuildTempDir()
		err := os.MkdirAll(buildDir, 0o755)
		require.NoError(t, err)

		// Create some tracking files
		file1 := filepath.Join(buildDir, "added_imports.1234.json")
		file2 := filepath.Join(buildDir, "added_imports.5678.json")
		require.NoError(t, os.WriteFile(file1, []byte("{}"), 0o644))
		require.NoError(t, os.WriteFile(file2, []byte("{}"), 0o644))

		// Also create a non-tracking file that should NOT be removed
		otherFile := filepath.Join(buildDir, "other.json")
		require.NoError(t, os.WriteFile(otherFile, []byte("{}"), 0o644))

		CleanupImportTrackingFiles()

		// Tracking files should be removed
		_, err = os.Stat(file1)
		assert.True(t, os.IsNotExist(err), "tracking file 1 should be removed")
		_, err = os.Stat(file2)
		assert.True(t, os.IsNotExist(err), "tracking file 2 should be removed")

		// Other file should still exist
		_, err = os.Stat(otherFile)
		assert.NoError(t, err, "non-tracking file should still exist")
	})

	t.Run("handles empty directory gracefully", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, tempDir)

		// Create build temp directory (empty)
		err := os.MkdirAll(util.GetBuildTempDir(), 0o755)
		require.NoError(t, err)

		// Should not panic
		CleanupImportTrackingFiles()
	})
}

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

func TestToolVersionLine(t *testing.T) {
	marker := "otelc@" + util.Version

	t.Run("release toolchain without rules hash", func(t *testing.T) {
		got := toolVersionLine("compile version go1.26.5", "")
		assert.Equal(t, "compile version go1.26.5 "+marker, got)
	})

	t.Run("release toolchain with rules hash", func(t *testing.T) {
		got := toolVersionLine("compile version go1.26.5", "abcd1234")
		assert.Equal(t, "compile version go1.26.5 "+marker+"/abcd1234", got)
	})

	t.Run("devel toolchain changes the content ID used by Go", func(t *testing.T) {
		line := "compile version devel go1.27-abc123 buildID=x/y/z"
		got := toolVersionLine(line, "abcd1234")
		assert.Equal(t, "compile version devel go1.27-abc123 buildID=x/y/z+"+marker+"+abcd1234", got)
		contentID := got[strings.LastIndex(got, "/")+1:]
		assert.Equal(t, "z+"+marker+"+abcd1234", contentID)
	})
}

func TestMarkedToolVersion(t *testing.T) {
	const raw = "compile version go1.26.5\n"

	t.Run("no rules hash when matched.json is absent", func(t *testing.T) {
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

		got := markedToolVersion(raw)
		assert.Equal(t, "compile version go1.26.5 otelc@"+util.Version, got)
	})

	t.Run("appends a 16-hex-digit rules hash when matched.json is present", func(t *testing.T) {
		workDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, workDir)
		require.NoError(t, os.MkdirAll(filepath.Join(workDir, util.BuildTempDir), 0o755))
		require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), []byte(`[{"module_path":"main"}]`), 0o644))

		got := markedToolVersion(raw)
		assert.Regexp(t, `^compile version go1\.26\.5 otelc@\S+/[0-9a-f]{16}$`, got)
	})
}

func TestEnableNestedToolexec(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)

	t.Run("appends to existing GOFLAGS and sets the nested marker", func(t *testing.T) {
		t.Setenv("GOFLAGS", "-mod=mod")
		t.Setenv(util.EnvOtelcNestedToolexec, "")

		require.NoError(t, EnableNestedToolexec())

		goflags := os.Getenv("GOFLAGS")
		assert.Contains(t, goflags, "-mod=mod", "existing flags are preserved")
		assert.Contains(t, goflags, "'-toolexec="+exe+" toolexec'", "otelc is added as the toolexec")
		assert.Equal(t, "1", os.Getenv(util.EnvOtelcNestedToolexec))
	})

	t.Run("handles empty GOFLAGS without leading whitespace", func(t *testing.T) {
		t.Setenv("GOFLAGS", "")
		t.Setenv(util.EnvOtelcNestedToolexec, "")

		require.NoError(t, EnableNestedToolexec())

		assert.Equal(t, "'-toolexec="+exe+" toolexec'", os.Getenv("GOFLAGS"))
	})
}

// versionMarkerPattern documents the exact tool-ID shape the go build cache
// keys on, guarding against accidental format drift.
var versionMarkerPattern = regexp.MustCompile(`otelc@[^\s/]+(/[0-9a-f]{16})?`)

func TestToolVersionLineMatchesCachePattern(t *testing.T) {
	assert.Regexp(t, versionMarkerPattern, toolVersionLine("compile version go1.26.5", ""))
	assert.Regexp(t, versionMarkerPattern, toolVersionLine("compile version go1.26.5", "0123456789abcdef"))
}
