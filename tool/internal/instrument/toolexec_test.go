// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/imports"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			result := stripCompleteFlag(tt.args)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUpdateImportConfig(t *testing.T) {
	t.Run("no importcfg path", func(t *testing.T) {
		ip := &InstrumentPhase{
			ctx:              t.Context(),
			importConfigPath: "",
		}
		err := ip.updateImportConfig(map[string]string{"fmt": "fmt"})
		require.NoError(t, err)
	})

	t.Run("empty new imports", func(t *testing.T) {
		tempDir := t.TempDir()
		cfgPath := filepath.Join(tempDir, "importcfg")
		err := os.WriteFile(cfgPath, []byte("packagefile fmt=/path/to/fmt.a\n"), 0o644)
		require.NoError(t, err)

		ip := &InstrumentPhase{
			ctx:              t.Context(),
			importConfigPath: cfgPath,
			importConfig: imports.ImportConfig{
				PackageFile: map[string]string{"fmt": "/path/to/fmt.a"},
			},
		}
		err = ip.updateImportConfig(map[string]string{})
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
			ctx:              t.Context(),
			importConfigPath: cfgPath,
			importConfig: imports.ImportConfig{
				PackageFile: map[string]string{"fmt": "/path/to/fmt.a"},
			},
		}
		err = ip.updateImportConfig(map[string]string{"unsafe": "unsafe"})
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
			ctx:              t.Context(),
			importConfigPath: cfgPath,
			importConfig: imports.ImportConfig{
				PackageFile: map[string]string{"fmt": "/path/to/fmt.a"},
			},
		}
		err = ip.updateImportConfig(map[string]string{"C": "C"})
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
			ctx:              t.Context(),
			importConfigPath: cfgPath,
			importConfig: imports.ImportConfig{
				PackageFile: map[string]string{"fmt": "/path/to/fmt.a"},
			},
		}
		err = ip.updateImportConfig(map[string]string{"fmt": "fmt"})
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
			ctx:              t.Context(),
			importConfigPath: cfgPath,
			importConfig: imports.ImportConfig{
				PackageFile: nil, // Intentionally nil
			},
		}

		// Should not panic, even though we're trying to add imports
		err = ip.updateImportConfig(map[string]string{"unsafe": "unsafe"})
		require.NoError(t, err)
	})

}

func TestTrackAddedImports(t *testing.T) {
	t.Run("empty packages does nothing", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelWorkDir, tempDir)

		err := trackAddedImports(map[string]string{})
		require.NoError(t, err)

		// No file should be created
		pattern := util.GetAddedImportsPattern()
		files, _ := filepath.Glob(pattern)
		assert.Empty(t, files)
	})

	t.Run("creates per-process file", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelWorkDir, tempDir)

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
		t.Setenv(util.EnvOtelWorkDir, tempDir)

		// Create build temp directory (empty)
		err := os.MkdirAll(util.GetBuildTempDir(), 0o755)
		require.NoError(t, err)

		result, err := loadAddedImports()
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("merges multiple per-process files", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelWorkDir, tempDir)

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

		result, err := loadAddedImports()
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
		t.Setenv(util.EnvOtelWorkDir, tempDir)

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

		result, err := loadAddedImports()
		require.NoError(t, err)

		// Should still get the valid import
		expected := map[string]string{"fmt": "/path/to/fmt.a"}
		assert.Equal(t, expected, result)
	})

	t.Run("later file overrides earlier for same package", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelWorkDir, tempDir)

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

		result, err := loadAddedImports()
		require.NoError(t, err)

		// The second file (lexically) should win
		assert.Equal(t, "/new/path/fmt.a", result["fmt"])
	})
}

func TestCleanupImportTrackingFiles(t *testing.T) {
	t.Run("removes all tracking files", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelWorkDir, tempDir)

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
		t.Setenv(util.EnvOtelWorkDir, tempDir)

		// Create build temp directory (empty)
		err := os.MkdirAll(util.GetBuildTempDir(), 0o755)
		require.NoError(t, err)

		// Should not panic
		CleanupImportTrackingFiles()
	})
}
