// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"bytes"
	"encoding/json"
	"go/build"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestPGOBuildFlags(t *testing.T) {
	const profile = "profile with spaces.pgo"
	const compileProfile = "-pgoprofile=compiled.pgo"
	tests := []struct {
		name        string
		compileArgs []string
		profile     string
		missing     bool
		readError   bool
		want        []string
	}{
		{
			name: "profile applied", compileArgs: []string{compileProfile}, profile: profile,
			want: []string{"-pgo=" + profile},
		},
		{
			name: "separated compile flag", compileArgs: []string{"-pgoprofile", "compiled.pgo"}, profile: profile,
			want: []string{"-pgo=" + profile},
		},
		{name: "unprofiled compile", profile: profile},
		{name: "empty profile", compileArgs: []string{compileProfile}},
		{name: "missing profile", compileArgs: []string{compileProfile}, missing: true},
		{name: "read error", compileArgs: []string{compileProfile}, readError: true},
		{name: "unprofiled compile does not read", readError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
			require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
			if tt.readError {
				require.NoError(t, os.Mkdir(util.GetPGOProfileFile(), 0o755))
			} else if !tt.missing {
				require.NoError(t, os.WriteFile(util.GetPGOProfileFile(), []byte(tt.profile), 0o644))
			}
			ip := &instrumentPhase{compileArgs: tt.compileArgs}
			flags, err := ip.pgoBuildFlags()
			if tt.readError && len(tt.compileArgs) > 0 {
				require.ErrorContains(t, err, "failed to read PGO profile")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, flags)
		})
	}
}

// TestPGOBuildFlagsForLinkCommand covers the link-side decision. A link
// command never carries -pgoprofile, because the profile shapes compile
// actions only, so the decision must follow the profile setup recorded; the
// compile-side table above cannot express that shape.
func TestPGOBuildFlagsForLinkCommand(t *testing.T) {
	const profile = "warm profile.pgo"
	linkTool := linkToolPath()
	args := []string{linkTool, "-o", "app.test", "-buildid=x", "-importcfg", "importcfg.link"}

	tests := []struct {
		name      string
		profile   string
		missing   bool
		readError bool
		want      []string
		wantErr   string
	}{
		{
			name: "recorded profile applies to the link", profile: profile,
			want: []string{"-pgo=" + profile},
		},
		{name: "empty recorded profile"},
		{name: "missing profile file", missing: true},
		{name: "read error", readError: true, wantErr: "failed to read PGO profile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
			require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
			if tt.readError {
				require.NoError(t, os.Mkdir(util.GetPGOProfileFile(), 0o755))
			} else if !tt.missing {
				require.NoError(t, os.WriteFile(util.GetPGOProfileFile(), []byte(tt.profile), 0o644))
			}

			flags, err := pgoBuildFlagsFor(args)

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, flags)
		})
	}
}

func TestUpdateImportConfigPGOReadError(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetPGOProfileFile(), 0o755))
	ip := &instrumentPhase{
		compileArgs:      []string{"-pgoprofile=compiled.pgo"},
		importConfigPath: filepath.Join(t.TempDir(), "importcfg"),
		importConfig:     imports.ImportConfig{PackageFile: map[string]string{"fmt": "fmt.a"}},
	}
	err := ip.updateImportConfig(t.Context(), map[string]string{"x": "example.com/x"})
	require.ErrorContains(t, err, "failed to read PGO profile")
}

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

func TestInterceptVetUsesPreservedCgoSource(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "vet.cfg")
	cgoPath := filepath.Join(tempDir, "source.cgo1.go")
	vetPath := cgoVetSourcePath(cgoPath)
	require.NoError(t, os.WriteFile(vetPath, []byte("package example"), 0o600))
	inputConfig := map[string]any{
		"GoFiles":    []string{"source.go", cgoPath},
		"ImportPath": "example.com/cgo",
	}
	data, err := json.Marshal(inputConfig)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, data, 0o600))

	args := []string{vetToolName, "-tests", configPath}
	got, err := interceptVet(t.Context(), args)
	require.NoError(t, err)
	assert.Equal(t, args, got)

	data, err = os.ReadFile(configPath)
	require.NoError(t, err)
	var config map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &config))

	var goFiles []string
	require.NoError(t, json.Unmarshal(config["GoFiles"], &goFiles))
	assert.Equal(t, []string{"source.go", vetPath}, goFiles)
	assert.JSONEq(t, `"example.com/cgo"`, string(config["ImportPath"]))
}

func TestInterceptVetIgnoresCgoSourceWithoutPreservedCopy(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "vet.cfg")
	original := []byte(`{"GoFiles":["source.go","source.cgo1.go"]}`)
	require.NoError(t, os.WriteFile(configPath, original, 0o600))

	_, err := interceptVet(t.Context(), []string{vetToolName, configPath})
	require.NoError(t, err)

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, original, data)
}

func TestInterceptVetLogsUnexpectedArguments(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := util.ContextWithLogger(t.Context(), logger)
	args := []string{vetToolName, "-tests"}

	got, err := interceptVet(ctx, args)

	require.NoError(t, err)
	assert.Equal(t, args, got)
	assert.Contains(t, logs.String(), "vet invocation missing expected vet.cfg argument")
}

// TestInterceptVetWithoutGoFiles covers a vet config that carries no GoFiles
// key. There is no source list to rewrite, so the command passes through; the
// previous code indexed the missing key and reported the resulting nil as
// malformed JSON, failing the build.
func TestInterceptVetWithoutGoFiles(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.New(slog.DiscardHandler))

	t.Run("absent key passes through", func(t *testing.T) {
		cfg := filepath.Join(t.TempDir(), "vet.cfg")
		require.NoError(t, os.WriteFile(cfg,
			[]byte(`{"Compiler":"gc","ImportPath":"example.com/p"}`), 0o644))
		args := []string{vetToolName, cfg}

		got, err := interceptVet(ctx, args)

		require.NoError(t, err)
		assert.Equal(t, args, got)
	})

	t.Run("malformed value still errors", func(t *testing.T) {
		cfg := filepath.Join(t.TempDir(), "vet.cfg")
		require.NoError(t, os.WriteFile(cfg,
			[]byte(`{"GoFiles":"not-a-list"}`), 0o644))

		_, err := interceptVet(ctx, []string{vetToolName, cfg})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing GoFiles from vet config")
	})
}

func TestUpdateImportConfig(t *testing.T) {
	t.Run("no importcfg path", func(t *testing.T) {
		ip := &instrumentPhase{
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

		ip := &instrumentPhase{
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

		ip := &instrumentPhase{
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

		ip := &instrumentPhase{
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

		ip := &instrumentPhase{
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

		ip := &instrumentPhase{
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
		files, err := addedImportFiles()
		require.NoError(t, err)
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

	t.Run("missing build directory returns empty map", func(t *testing.T) {
		// No .otelc-build directory was ever created.
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

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
		// Note: os.ReadDir returns entries in lexical order
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

	t.Run("ignores entries that do not match the tracking pattern", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, tempDir)

		buildDir := util.GetBuildTempDir()
		require.NoError(t, os.MkdirAll(buildDir, 0o755))

		validData, _ := json.Marshal(map[string]string{"fmt": "/path/to/fmt.a"})
		require.NoError(t, os.WriteFile(filepath.Join(buildDir, "added_imports.1.json"), validData, 0o644))

		// None of these should be picked up: wrong suffix or wrong prefix.
		require.NoError(t, os.WriteFile(filepath.Join(buildDir, "added_imports.1.txt"), validData, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(buildDir, "other_added_imports.1.json"), validData, 0o644))

		result, err := loadAddedImports(t.Context())
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"fmt": "/path/to/fmt.a"}, result)
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

	t.Run("handles missing directory gracefully", func(t *testing.T) {
		// The very first build has no .otelc-build directory yet.
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

		// Should not panic
		CleanupImportTrackingFiles()
	})
}

func TestLoadAddedImportsWithGlobMetaCharactersInPath(t *testing.T) {
	// A workspace path containing glob meta-characters (here a character
	// class) must not stop the tracking files from being discovered.
	workDir := filepath.Join(t.TempDir(), "project-[dev]")
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	writeAddedImports(t, map[string]string{"fmt": "/path/to/fmt.a"})

	result, err := loadAddedImports(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"fmt": "/path/to/fmt.a"}, result)
}

func TestCleanupImportTrackingFilesWithGlobMetaCharactersInPath(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "project-[dev]")
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	buildDir := util.GetBuildTempDir()
	require.NoError(t, os.MkdirAll(buildDir, 0o755))
	trackingFile := filepath.Join(buildDir, "added_imports.1.json")
	require.NoError(t, os.WriteFile(trackingFile, []byte("{}"), 0o644))

	CleanupImportTrackingFiles()

	_, err := os.Stat(trackingFile)
	assert.True(t, os.IsNotExist(err), "tracking file should be removed")
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

	ip := &instrumentPhase{
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
	assert.Contains(t, err.Error(), "importcfg")
}

func TestInterceptCompileImportNamesLoadError(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), []byte(`[]`), 0o644))
	require.NoError(t, os.WriteFile(util.GetImportNamesFile(), []byte("not json"), 0o644))

	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	args := []string{
		"compile",
		"-o", filepath.Join(t.TempDir(), "out.a"),
		"-p", "main",
	}
	_, err := interceptCompile(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestUpdateImportConfigAddsResolvedImport(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	cfgPath := filepath.Join(workDir, "importcfg")
	require.NoError(t, os.WriteFile(cfgPath, []byte("packagefile context=/unused/context.a\n"), 0o644))

	ip := &instrumentPhase{
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

	ip := &instrumentPhase{
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
	ip := &instrumentPhase{
		logger:           slog.Default(),
		importConfigPath: cfgPath,
		importConfig: imports.ImportConfig{
			PackageFile: map[string]string{},
		},
	}
	err := ip.updateImportConfig(t.Context(), map[string]string{"fmt": "fmt"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create temporary file")
}

func TestUpdateImportConfigTrackError(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	// No .otelc-build directory is created, so the per-process tracking file
	// write fails after the importcfg is updated. That failure is non-fatal.
	cfgPath := filepath.Join(workDir, "importcfg")
	require.NoError(t, os.WriteFile(cfgPath, []byte(""), 0o644))

	ip := &instrumentPhase{
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

func TestLoadAddedImportsScanError(t *testing.T) {
	if runtime.GOOS == "windows" {
		// On Windows a regular file used as a directory reports a missing
		// path, which loadAddedImports treats like an absent directory.
		t.Skip("directory-scan error cannot be triggered on Windows")
	}
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	// A regular file where the build temp directory is expected makes the
	// directory scan fail with a non-not-exist error.
	require.NoError(t, os.WriteFile(util.GetBuildTempDir(), []byte("not a directory"), 0o644))

	_, err := loadAddedImports(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading build temp directory")
}

func TestLoadAddedImportsIgnoresDirectoryEntry(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	buildDir := util.GetBuildTempDir()
	require.NoError(t, os.MkdirAll(buildDir, 0o755))
	// A directory whose name matches the tracking-file pattern is ignored.
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
	if runtime.GOOS == "windows" {
		// On Windows a regular file used as a directory reports a missing
		// path, which loadAddedImports treats like an absent directory.
		t.Skip("directory-scan error cannot be triggered on Windows")
	}
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	// A regular file where the build temp directory is expected makes
	// loadAddedImports fail. That failure is not fatal: the link still
	// recovers rule-added imports from matched.json. With no matched rules
	// and nothing to add, the link command is passed through unchanged.
	require.NoError(t, os.WriteFile(util.GetBuildTempDir(), []byte("not a directory"), 0o644))

	cfgPath := filepath.Join(workDir, "importcfg.link")
	require.NoError(t, os.WriteFile(cfgPath, []byte("packagefile fmt=/unused/fmt.a\n"), 0o644))
	args := []string{"link", "-o", "exe", "-buildid", "id", "-importcfg", cfgPath}
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
	assert.Contains(t, err.Error(), "importcfg")
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

func TestResolvePackageName(t *testing.T) {
	dir := t.TempDir()
	testMain := filepath.Join(dir, "_testmain.go")
	require.NoError(t, os.WriteFile(testMain,
		[]byte("// Code generated by 'go test'. DO NOT EDIT.\n\npackage main\n"), 0o644))

	t.Run("reads the package clause from the compile sources", func(t *testing.T) {
		ip := &instrumentPhase{compileArgs: []string{
			"compile", "-o", "_pkg_.a", "-p", "main", "-pack", testMain,
		}}
		name, err := ip.resolvePackageName()
		require.NoError(t, err)
		assert.Equal(t, "main", name)
	})

	t.Run("prefers the package clause over the -p import path", func(t *testing.T) {
		ip := &instrumentPhase{compileArgs: []string{
			"compile", "-o", "_pkg_.a", "-p", "example.com/foo", "-pack", testMain,
		}}
		name, err := ip.resolvePackageName()
		require.NoError(t, err)
		assert.Equal(t, "main", name)
	})

	t.Run("skips flag values that are not readable Go files", func(t *testing.T) {
		ip := &instrumentPhase{compileArgs: []string{
			"compile", "-o", filepath.Join(dir, "absent.go"), "-pack", testMain,
		}}
		name, err := ip.resolvePackageName()
		require.NoError(t, err)
		assert.Equal(t, "main", name)
	})

	t.Run("returns empty when the command lists no Go source", func(t *testing.T) {
		ip := &instrumentPhase{compileArgs: []string{"compile", "-o", "_pkg_.a", "-p", "main"}}
		name, err := ip.resolvePackageName()
		require.NoError(t, err)
		assert.Empty(t, name)
	})

	t.Run("propagates a parse failure", func(t *testing.T) {
		broken := filepath.Join(dir, "broken.go")
		require.NoError(t, os.WriteFile(broken, []byte("not a go file"), 0o644))
		ip := &instrumentPhase{compileArgs: []string{"compile", broken}}
		_, err := ip.resolvePackageName()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing package clause")
	})
}

func TestInterceptCompileFileRuleWithoutPackageName(t *testing.T) {
	setup := func(t *testing.T, sources ...string) (string, []string) {
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

		rulesDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(rulesDir, "added.go"),
			[]byte("//go:build ignore\n\npackage myrules\n\nfunc init() {}\n"), 0o644))

		// Setup hands over an empty PackageName for the generated test main.
		set := rule.NewInstRuleSet("main")
		fileRule := &rule.InstFileRule{File: "added.go", Path: rulesDir, ResolvedPath: rulesDir}
		fileRule.Name = "add_to_main"
		set.AddFileRule(fileRule)
		// Setup records whether the compile belongs to a test binary.
		set.IsTest = util.IsTestBuild(sources)
		data, err := json.Marshal([]*rule.InstRuleSet{set})
		require.NoError(t, err)
		matched := util.GetMatchedRuleFile()
		require.NoError(t, os.MkdirAll(filepath.Dir(matched), 0o755))
		require.NoError(t, os.WriteFile(matched, data, 0o644))

		buildDir := t.TempDir()
		args := make([]string, 0, 7+len(sources))
		args = append(args, "compile", "-o", filepath.Join(buildDir, "_pkg_.a"), "-p", "main", "-buildid", "x")
		for _, name := range sources {
			path := filepath.Join(buildDir, name)
			require.NoError(t, os.WriteFile(path, []byte("package main\n"), 0o644))
			args = append(args, path)
		}
		return buildDir, args
	}

	t.Run("resolves the name from the compile sources", func(t *testing.T) {
		buildDir, args := setup(t, "_testmain.go")
		ctx := util.ContextWithLogger(t.Context(), slog.New(slog.DiscardHandler))

		newArgs, err := interceptCompile(ctx, args)
		require.NoError(t, err)

		added := filepath.Join(buildDir, "otelc.added.go")
		assert.Contains(t, newArgs, added)
		name, err := ast.ParsePackageName(added)
		require.NoError(t, err)
		assert.Equal(t, "main", name)
	})

	t.Run("errors when there is nothing to resolve from", func(t *testing.T) {
		buildDir, args := setup(t)
		ctx := util.ContextWithLogger(t.Context(), slog.New(slog.DiscardHandler))

		_, err := interceptCompile(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot resolve the package name")
		assert.NoFileExists(t, filepath.Join(buildDir, "otelc.added.go"))
	})

	t.Run("propagates a parse failure", func(t *testing.T) {
		buildDir, args := setup(t, "_testmain.go")
		broken := filepath.Join(buildDir, "_testmain.go")
		require.NoError(t, os.WriteFile(broken, []byte("not a go file"), 0o644))
		ctx := util.ContextWithLogger(t.Context(), slog.New(slog.DiscardHandler))

		_, err := interceptCompile(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing package clause")
		assert.NoFileExists(t, filepath.Join(buildDir, "otelc.added.go"))
	})
}

func TestInterceptLinkWriteError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod permissions are not enforced consistently on Windows")
	}

	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	writeAddedImports(t, map[string]string{"fmt": "/new/fmt.a"})

	cfgDir := filepath.Join(workDir, "linkcfg")
	require.NoError(t, os.Mkdir(cfgDir, 0o755))
	linkCfg := filepath.Join(cfgDir, "importcfg.link")
	require.NoError(t, os.WriteFile(linkCfg, []byte("packagefile context=/old/context.a\n"), 0o644))
	// The rewrite is atomic, so it needs to create a temporary file alongside
	// the target. Make the directory read-only so that creation fails.
	require.NoError(t, os.Chmod(cfgDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(cfgDir, 0o755) })

	args := []string{"link", "-o", "exe", "-buildid", "id", "-importcfg", linkCfg}
	_, err := interceptLink(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create temporary file")
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

	t.Run("build tags change the rules hash", func(t *testing.T) {
		workDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, workDir)
		t.Setenv("GOFLAGS", "")
		require.NoError(t, os.MkdirAll(filepath.Join(workDir, util.BuildTempDir), 0o755))
		require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), []byte(`[{"module_path":"main"}]`), 0o644))

		t.Setenv(util.EnvOtelcBuildFlags, util.EncodeBuildFlags(nil))
		untagged := markedToolVersion(raw)
		t.Setenv(util.EnvOtelcBuildFlags, util.EncodeBuildFlags([]string{"-tags=enterprise"}))
		tagged := markedToolVersion(raw)
		t.Setenv(util.EnvOtelcBuildFlags, util.EncodeBuildFlags([]string{"-tags=beta"}))
		otherTag := markedToolVersion(raw)

		assert.NotEqual(t, untagged, tagged)
		assert.NotEqual(t, tagged, otherTag)
	})
}

func TestEnableNestedToolexec(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)

	// Built rather than hardcoded: the quoting depends on whether the test
	// binary's own path contains a space, which varies by machine.
	wantToken, err := nestedToolexecGoflagsToken(exe)
	require.NoError(t, err)

	t.Run("appends to existing GOFLAGS and sets the nested marker", func(t *testing.T) {
		t.Setenv("GOFLAGS", "-mod=mod")
		t.Setenv(util.EnvOtelcNestedToolexec, "")

		require.NoError(t, EnableNestedToolexec())

		goflags := os.Getenv("GOFLAGS")
		assert.Contains(t, goflags, "-mod=mod", "existing flags are preserved")
		assert.Contains(t, goflags, wantToken, "otelc is added as the toolexec")
		assert.Equal(t, "1", os.Getenv(util.EnvOtelcNestedToolexec))
	})

	t.Run("strips only unpatterned gcflags from GOFLAGS", func(t *testing.T) {
		t.Setenv("GOFLAGS", "-mod=mod -gcflags=-l '-gcflags=all=-N -l'")
		t.Setenv(util.EnvOtelcNestedToolexec, "")

		require.NoError(t, EnableNestedToolexec())

		tokens := util.SplitGoflags(os.Getenv("GOFLAGS"))
		assert.Contains(t, tokens, "-mod=mod")
		assert.Contains(t, tokens, "'-gcflags=all=-N -l'")
		assert.NotContains(t, tokens, "-gcflags=-l")
		assert.Contains(t, tokens, wantToken)
	})

	t.Run("handles empty GOFLAGS without leading whitespace", func(t *testing.T) {
		t.Setenv("GOFLAGS", "")
		t.Setenv(util.EnvOtelcNestedToolexec, "")

		require.NoError(t, EnableNestedToolexec())

		assert.Equal(t, wantToken, os.Getenv("GOFLAGS"))
	})

	t.Run("GOFLAGS survives both splits cmd/go performs", func(t *testing.T) {
		t.Setenv("GOFLAGS", "")
		t.Setenv(util.EnvOtelcNestedToolexec, "")

		require.NoError(t, EnableNestedToolexec())

		assert.Equal(t, []string{exe, "toolexec"}, parseToolexecFromGoflags(t, os.Getenv("GOFLAGS")))
	})

	t.Run("a spaced executable path survives both splits", func(t *testing.T) {
		// EnableNestedToolexec resolves its own path, so the spaced case is
		// exercised by calling the extracted helper directly and parsing the
		// result back the way cmd/go would.
		spaced := filepath.Join(string(filepath.Separator)+"opt", "my tools", "otelc")
		token, tokenErr := nestedToolexecGoflagsToken(spaced)
		require.NoError(t, tokenErr)

		assert.Equal(t, []string{spaced, "toolexec"}, parseToolexecFromGoflags(t, token))
	})

	t.Run("a spaced path carrying either quote is rejected", func(t *testing.T) {
		// Whitespace forces the inner quoting to pick one quote character, and
		// a quote already in the path supplies the other. Either kind gets us
		// there, so the rejection is not specific to single quotes.
		for _, path := range []string{`/home/it's me/otelc`, `/home/my "tools"/otelc`} {
			_, tokenErr := nestedToolexecGoflagsToken(path)
			require.Error(t, tokenErr, "%q should not be representable in GOFLAGS", path)
			assert.Contains(t, tokenErr.Error(), "cannot be passed to nested go commands through GOFLAGS")
		}
	})

	t.Run("a spaced path already carrying both quotes fails building the inner flag", func(t *testing.T) {
		// Distinct from the case above: here execPath itself has both quote
		// characters plus whitespace, so BuildToolexecFlag's own quoting of
		// execPath fails before the inner/outer nesting is even attempted.
		// nestedToolexecGoflagsToken adds no message of its own for this one,
		// BuildToolexecFlag's error already names execPath, so this asserts on
		// its wording rather than anything added downstream.
		_, tokenErr := nestedToolexecGoflagsToken(`/home/it's "here"/otelc`)
		require.Error(t, tokenErr)
		assert.Contains(t, tokenErr.Error(), "quoting otelc executable path for -toolexec")
	})

	t.Run("a path with an interior quote but no space survives both splits", func(t *testing.T) {
		// quoted.Split reads an interior quote literally, so this path needs no
		// quoting and must keep working rather than being rejected.
		quoted := "/opt/it's/otelc"
		token, tokenErr := nestedToolexecGoflagsToken(quoted)
		require.NoError(t, tokenErr)

		assert.Equal(t, []string{quoted, "toolexec"}, parseToolexecFromGoflags(t, token))
	})

	t.Run("returns error when executable path cannot be quoted in GOFLAGS", func(t *testing.T) {
		originalExe := executablePath
		t.Cleanup(func() { executablePath = originalExe })
		executablePath = func() (string, error) {
			return `/home/it's "here"/otelc`, nil
		}

		enableErr := EnableNestedToolexec()
		require.Error(t, enableErr)
	})
}

// splitQuoted mirrors cmd/internal/quoted.Split, which is what cmd/go uses for
// both GOFLAGS and the -toolexec value: whitespace-separated, with `'` or `"`
// grouping, and no unescaping inside a quoted run. Reimplemented here rather
// than reusing the production helpers so the tests below check the round trip
// against the real format instead of against otelc's own encoder.
func splitQuoted(t *testing.T, s string) []string {
	t.Helper()
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
	var out []string
	for len(s) > 0 {
		for len(s) > 0 && isSpace(s[0]) {
			s = s[1:]
		}
		if len(s) == 0 {
			break
		}
		if q := s[0]; q == '"' || q == '\'' {
			s = s[1:]
			end := strings.IndexByte(s, q)
			require.GreaterOrEqual(t, end, 0, "unterminated %c-quoted run in %q", q, s)
			out = append(out, s[:end])
			s = s[end+1:]
			continue
		}
		end := 0
		for end < len(s) && !isSpace(s[end]) {
			end++
		}
		out = append(out, s[:end])
		s = s[end:]
	}
	return out
}

// parseToolexecFromGoflags walks the same two split layers cmd/go does and
// returns the argv it would end up executing for -toolexec.
func parseToolexecFromGoflags(t *testing.T, goflags string) []string {
	t.Helper()
	flags := splitQuoted(t, goflags)
	require.Len(t, flags, 1, "expected a lone -toolexec flag in %q", goflags)
	value, ok := strings.CutPrefix(flags[0], "-toolexec=")
	require.True(t, ok, "flag %q is not a -toolexec entry", flags[0])
	return splitQuoted(t, value)
}

// versionMarkerPattern documents the exact tool-ID shape the go build cache
// keys on, guarding against accidental format drift.
var versionMarkerPattern = regexp.MustCompile(`otelc@[^\s/]+(/[0-9a-f]{16})?`)

func TestToolVersionLineMatchesCachePattern(t *testing.T) {
	assert.Regexp(t, versionMarkerPattern, toolVersionLine("compile version go1.26.5", ""))
	assert.Regexp(t, versionMarkerPattern, toolVersionLine("compile version go1.26.5", "0123456789abcdef"))
}

func TestUpdateImportConfigStopsAddedImportCycle(t *testing.T) {
	// The outer build compiles example.com/p and resolves example.com/q, an
	// import a rule added to it. The nested build compiles example.com/d, a
	// dependency of q, and a rule adds an import of p to it. Resolving p would
	// compile p again, add q again, and never end.
	t.Setenv(util.EnvOtelcNestedResolving, "example.com/p>example.com/q")
	cfgPath := filepath.Join(t.TempDir(), "importcfg")
	require.NoError(t, os.WriteFile(cfgPath, []byte("packagefile fmt=/path/to/fmt.a\n"), 0o644))
	ip := &instrumentPhase{
		importConfigPath: cfgPath,
		compileArgs:      []string{"compile", "-p", "example.com/d"},
	}

	err := ip.updateImportConfig(t.Context(), map[string]string{"p": "example.com/p"})

	require.Error(t, err)
	assert.Contains(t, err.Error(),
		"rules add an import cycle: example.com/p adds example.com/q; example.com/d adds example.com/p")
}

func TestEnterNestedResolution(t *testing.T) {
	t.Run("extends the chain for the nested build and restores it", func(t *testing.T) {
		t.Setenv(util.EnvOtelcNestedResolving, "example.com/p>example.com/q")
		ip := &instrumentPhase{compileArgs: []string{"compile", "-p", "example.com/d"}}

		restore, err := ip.enterNestedResolution("example.com/x")
		require.NoError(t, err)
		assert.Equal(t, "example.com/p>example.com/q,example.com/d>example.com/x",
			os.Getenv(util.EnvOtelcNestedResolving))

		restore()
		assert.Equal(t, "example.com/p>example.com/q", os.Getenv(util.EnvOtelcNestedResolving))
	})

	t.Run("unsets the chain after the outermost resolution", func(t *testing.T) {
		t.Setenv(util.EnvOtelcNestedResolving, "")
		require.NoError(t, os.Unsetenv(util.EnvOtelcNestedResolving))
		ip := &instrumentPhase{compileArgs: []string{"compile", "-p", "example.com/p"}}

		restore, err := ip.enterNestedResolution("example.com/q")
		require.NoError(t, err)
		assert.Equal(t, "example.com/p>example.com/q", os.Getenv(util.EnvOtelcNestedResolving))

		restore()
		_, set := os.LookupEnv(util.EnvOtelcNestedResolving)
		assert.False(t, set)
	})

	t.Run("a package adding itself is a cycle", func(t *testing.T) {
		t.Setenv(util.EnvOtelcNestedResolving, "")
		ip := &instrumentPhase{compileArgs: []string{"compile", "-p", "example.com/p"}}

		_, err := ip.enterNestedResolution("example.com/p")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rules add an import cycle: example.com/p adds example.com/p")
	})

	t.Run("a chain past the depth bound fails", func(t *testing.T) {
		// Each entry waits on a package no other entry compiles, so no cycle
		// closes, yet the chain grows without end.
		entries := make([]string, maxResolutionChain)
		for i := range entries {
			entries[i] = "example.com/p" + strconv.Itoa(i) + ">example.com/q" + strconv.Itoa(i)
		}
		t.Setenv(util.EnvOtelcNestedResolving, strings.Join(entries, ","))
		ip := &instrumentPhase{compileArgs: []string{"compile", "-p", "example.com/deep"}}

		_, err := ip.enterNestedResolution("example.com/added")
		require.Error(t, err)
		assert.Contains(t, err.Error(),
			"rules add imports nested deeper than "+strconv.Itoa(maxResolutionChain))
		assert.Contains(t, err.Error(), "example.com/deep adds example.com/added")
	})
}

func TestUserBuildTags(t *testing.T) {
	tests := []struct {
		name       string
		goflags    []string
		buildFlags []string
		expected   []string
	}{
		{name: "no tags", goflags: nil, buildFlags: []string{"-race"}, expected: nil},
		{name: "equals form", buildFlags: []string{"-tags=enterprise,beta"}, expected: []string{"enterprise", "beta"}},
		{name: "separate value", buildFlags: []string{"-tags", "enterprise"}, expected: []string{"enterprise"}},
		{name: "double dash", buildFlags: []string{"--tags=enterprise"}, expected: []string{"enterprise"}},
		{name: "space separated list", buildFlags: []string{"-tags", "a b"}, expected: []string{"a", "b"}},
		{name: "last flag wins", buildFlags: []string{"-tags=a", "-tags=b"}, expected: []string{"b"}},
		{name: "from GOFLAGS", goflags: []string{"-mod=mod", "-tags=enterprise"}, expected: []string{"enterprise"}},
		{
			name:       "command line overrides GOFLAGS",
			goflags:    []string{"-tags=fromenv"},
			buildFlags: []string{"-tags=fromcli"},
			expected:   []string{"fromcli"},
		},
		{name: "quoted GOFLAGS value", goflags: []string{`-tags="a,b"`}, expected: []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, util.UserBuildTags(tt.goflags, tt.buildFlags))
		})
	}
}

func TestToolexecEmptyArgs(t *testing.T) {
	err := Toolexec(t.Context(), []string{}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no tool command specified")
}

// callRuleWithImports returns a call rule whose JSON unmarshals cleanly; the
// selector must parse, so it names a real package function shape.
func callRuleWithImports(imports map[string]string) *rule.InstCallRule {
	return &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{
			Name: "call-rule", Imports: imports,
		},
		FunctionCall: "example.com/b.New",
	}
}

// writeMatchedRules stores matched.json the way setup does, so the link phase
// can read the rule-added imports back.
func writeMatchedRules(t *testing.T, sets []*rule.InstRuleSet) {
	t.Helper()
	content, err := json.Marshal(sets)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), content, 0o600))
}

// linkArgs returns a minimal link command with importcfg at cfgPath.
func linkArgs(cfgPath string) []string {
	return []string{"link", "-o", "out.test", "-buildid=test", "-importcfg", cfgPath}
}

// TestInterceptLinkRecoversRuleAddedImport covers the warm-GOCACHE shape: no
// compile ran, so no per-process tracking file exists, and the link must
// still receive the import a rule adds, recovered from matched.json.
func TestInterceptLinkRecoversRuleAddedImportFromMatchedRules(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	writeMatchedRules(t, []*rule.InstRuleSet{
		{
			PackageName: "dep",
			ModulePath:  "example.com/app/dep",
			FuncRules: map[string][]*rule.InstFuncRule{
				"dep.go": {{
					InstBaseRule: rule.InstBaseRule{
						Name:    "rule",
						Target:  rule.NewTarget("example.com/app/dep"),
						Imports: map[string]string{"extra": "fmt"},
					},
				}},
			},
		},
	})

	cfgPath := filepath.Join(workDir, "importcfg.link")
	// The link lists the rule set's target package, the shape a real link
	// importcfg always has: the target's compile is part of the binary.
	require.NoError(t, os.WriteFile(cfgPath,
		[]byte("packagefile context=/unused/context.a\npackagefile example.com/app/dep=/unused/dep.a\n"), 0o644))

	_, err := interceptLink(t.Context(), linkArgs(cfgPath))
	require.NoError(t, err)

	content, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "packagefile fmt=")
}

// TestInterceptLinkSkipsImportsTheLinkAlreadyHas checks that a link importcfg
// listing every rule-added import is left untouched, and that a build with no
// matched rules changes nothing.
func TestInterceptLinkLeavesCompleteImportcfgUnchanged(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	writeMatchedRules(t, []*rule.InstRuleSet{
		{
			PackageName: "dep",
			ModulePath:  "example.com/app/dep",
			FuncRules: map[string][]*rule.InstFuncRule{
				"dep.go": {{
					InstBaseRule: rule.InstBaseRule{
						Name:    "rule",
						Target:  rule.NewTarget("example.com/app/dep"),
						Imports: map[string]string{"extra": "fmt"},
					},
				}},
			},
		},
	})

	cfgPath := filepath.Join(workDir, "importcfg.link")
	original := "packagefile fmt=/unused/fmt.a\npackagefile context=/unused/context.a\npackagefile example.com/app/dep=/unused/dep.a\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(original), 0o644))

	_, err := interceptLink(t.Context(), linkArgs(cfgPath))
	require.NoError(t, err)

	content, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, original, string(content))

	// Without matched.json there is nothing to recover either.
	require.NoError(t, os.Remove(util.GetMatchedRuleFile()))
	_, err = interceptLink(t.Context(), linkArgs(cfgPath))
	require.NoError(t, err)
	content, err = os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, original, string(content))
}

// TestRuleAddedImportsCollectsEveryRuleKind checks that the link-side
// candidate set covers the imports of every rule kind and of add_file rule
// sources, and skips the built-in pseudo-packages.
func TestRuleAddedImportsCollectsEveryRuleKind(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	sourceDir := filepath.Join(workDir, "hooksrc")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	sourceFile := filepath.Join(sourceDir, "added.go")
	require.NoError(t, os.WriteFile(sourceFile, []byte(
		"//go:build ignore\n\npackage hooks\n\nimport (\n\t\"fmt\"\n\t\"example.com/hooks\"\n)\n",
	), 0o644))

	writeMatchedRules(t, []*rule.InstRuleSet{
		{
			PackageName: "dep",
			ModulePath:  "example.com/app/dep",
			FuncRules: map[string][]*rule.InstFuncRule{
				"dep.go": {{InstBaseRule: rule.InstBaseRule{
					Name: "func-rule", Imports: map[string]string{"a": "example.com/a"},
				}}},
			},
			CallRules: map[string][]*rule.InstCallRule{
				"dep.go": {callRuleWithImports(map[string]string{"b": "example.com/b"})},
			},
			RawRules: map[string][]*rule.InstRawRule{
				"dep.go": {{InstBaseRule: rule.InstBaseRule{
					Name: "raw-rule", Imports: map[string]string{"c": "unsafe"},
				}}},
			},
			FileRules: []*rule.InstFileRule{{
				InstBaseRule: rule.InstBaseRule{Name: "file-rule"},
				File:         "added.go",
				Path:         "example.com/app/hooks",
				ResolvedPath: sourceDir,
			}},
		},
	})

	paths := ruleAddedImports(t.Context(), nil, &imports.ImportConfig{
		PackageFile: map[string]string{"example.com/app/dep": "/unused/dep.a"},
	})
	assert.Equal(t, map[string]struct{}{
		"example.com/a":     {},
		"example.com/b":     {},
		"unsafe":            {},
		"fmt":               {},
		"example.com/hooks": {},
	}, paths)
}

// TestResolveRuleAddedImportsSkipsUnresolvableImports checks that a candidate
// the rules declare but that cannot load does not fail the link, while a
// resolvable one still lands in the import config.
func TestResolveRuleAddedImportsSkipsUnresolvableImports(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	linkConfig := imports.ImportConfig{
		PackageFile: map[string]string{
			"context":             "/unused/context.a",
			"example.com/app/dep": "/unused/dep.a",
		},
	}
	updated, err := resolveRuleAddedImports(t.Context(), nil, &linkConfig)
	require.NoError(t, err)
	assert.False(t, updated)

	writeMatchedRules(t, []*rule.InstRuleSet{
		{
			PackageName: "dep",
			ModulePath:  "example.com/app/dep",
			FuncRules: map[string][]*rule.InstFuncRule{
				"dep.go": {{InstBaseRule: rule.InstBaseRule{
					Name: "rule",
					Imports: map[string]string{
						"std":          "fmt",
						"unresolvable": "example.invalid/nonexistent/pkg",
					},
				}}},
			},
		},
	})

	updated, err = resolveRuleAddedImports(t.Context(), nil, &linkConfig)
	require.NoError(t, err)
	assert.True(t, updated)
	assert.Contains(t, linkConfig.PackageFile, "fmt")
	assert.NotContains(t, linkConfig.PackageFile, "example.invalid/nonexistent/pkg")
}

// TestResolveRuleAddedImportsRestrictedToLinkPackages checks that rule sets
// whose target package is not part of the link contribute no candidates: a
// multi-binary build must not resolve the imports of another binary's rules.
func TestResolveRuleAddedImportsRestrictedToLinkPackages(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	writeMatchedRules(t, []*rule.InstRuleSet{
		{
			PackageName: "a",
			ModulePath:  "example.com/app/a",
			FuncRules: map[string][]*rule.InstFuncRule{
				"a.go": {{InstBaseRule: rule.InstBaseRule{
					Name: "rule-a", Imports: map[string]string{"fmt": "fmt"},
				}}},
			},
		},
		{
			PackageName: "b",
			ModulePath:  "example.com/app/b",
			FuncRules: map[string][]*rule.InstFuncRule{
				"b.go": {{InstBaseRule: rule.InstBaseRule{
					Name: "rule-b", Imports: map[string]string{"strings": "strings"},
				}}},
			},
		},
	})

	// The link lists target a but not target b, so only a's imports may be
	// recovered. Both imports resolve, so only the filter can keep b's out.
	linkConfig := imports.ImportConfig{
		PackageFile: map[string]string{
			"context":           "/unused/context.a",
			"example.com/app/a": "/unused/a.a",
		},
	}
	updated, err := resolveRuleAddedImports(t.Context(), nil, &linkConfig)
	require.NoError(t, err)
	assert.True(t, updated)
	assert.Contains(t, linkConfig.PackageFile, "fmt")
	assert.NotContains(t, linkConfig.PackageFile, "strings",
		"the link does not list example.com/app/b, so its rule imports must not be resolved")
}

// TestResolveRuleAddedImportsSkipsConstraintExcludedAddFileSource checks that
// an add_file source the active build constraints exclude contributes no
// candidates. Its import resolves on this platform, so only the constraint
// check can keep it out of the link.
func TestResolveRuleAddedImportsSkipsConstraintExcludedAddFileSource(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	sourceDir := filepath.Join(workDir, "hooksrc")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "excluded.go"),
		[]byte("//go:build otelc_never_active\n\npackage hooks\n\nimport \"os\"\n"), 0o644))

	writeMatchedRules(t, []*rule.InstRuleSet{
		{
			PackageName: "dep",
			ModulePath:  "example.com/app/dep",
			FileRules: []*rule.InstFileRule{{
				InstBaseRule: rule.InstBaseRule{Name: "file-rule"},
				File:         "excluded.go",
				Path:         "example.com/app/hooks",
				ResolvedPath: sourceDir,
			}},
		},
	})

	linkConfig := imports.ImportConfig{
		PackageFile: map[string]string{
			"context":             "/unused/context.a",
			"example.com/app/dep": "/unused/dep.a",
		},
	}
	updated, err := resolveRuleAddedImports(t.Context(), nil, &linkConfig)
	require.NoError(t, err)
	assert.False(t, updated)
	assert.NotContains(t, linkConfig.PackageFile, "os",
		"the build constraints exclude the add_file source, so its imports must not be resolved")
}

// TestRuleAddedImportsCollectsStructLitDirectiveDeclRules covers the rule
// kinds the other candidate tests leave out: struct, literal, directive, and
// declaration rules. Every kind embeds InstBaseRule, whose imports the
// collector must read the same way it reads them for func, call, and raw
// rules.
func TestRuleAddedImportsCollectsStructLitDirectiveDeclRules(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	writeMatchedRules(t, []*rule.InstRuleSet{
		{
			PackageName: "dep",
			ModulePath:  "example.com/app/dep",
			StructRules: map[string][]*rule.InstStructRule{
				"dep.go": {{
					InstBaseRule: rule.InstBaseRule{
						Name: "struct-rule", Imports: map[string]string{"s": "example.com/s"},
					},
					Struct:   "Transport",
					NewField: []*rule.InstStructField{{Name: "F", Type: "int"}},
				}},
			},
			LitRules: map[string][]*rule.InstLitRule{
				"dep.go": {{
					InstBaseRule: rule.InstBaseRule{
						Name: "lit-rule", Imports: map[string]string{"l": "example.com/l"},
					},
					StructLiteral: "net/http.Transport",
					Field:         []*rule.InstLitField{{Name: "F", Value: "true"}},
				}},
			},
			DirectiveRules: map[string][]*rule.InstDirectiveRule{
				"dep.go": {{
					InstBaseRule: rule.InstBaseRule{
						Name: "directive-rule", Imports: map[string]string{"d": "example.com/d"},
					},
					Directive: "//otelc:span",
					Template:  "otel.Span()",
				}},
			},
			DeclRules: map[string][]*rule.InstDeclRule{
				"dep.go": {{
					InstBaseRule: rule.InstBaseRule{
						Name: "decl-rule", Imports: map[string]string{"decl": "example.com/decl"},
					},
					Kind:       "var",
					Identifier: "DefaultTransport",
					Replace:    "http.DefaultTransport",
				}},
			},
		},
	})

	paths := ruleAddedImports(t.Context(), nil, &imports.ImportConfig{
		PackageFile: map[string]string{"example.com/app/dep": "/unused/dep.a"},
	})
	assert.Equal(t, map[string]struct{}{
		"example.com/s":    {},
		"example.com/l":    {},
		"example.com/d":    {},
		"example.com/decl": {},
	}, paths)
}

// TestResolveRuleAddedImportsSkipsBuiltinPseudoPackages checks that rules
// declaring imports of unsafe or the cgo pseudo-package C contribute no
// resolvable candidate: neither has an archive, and asking the nested build
// for one would only log a warning.
func TestResolveRuleAddedImportsSkipsBuiltinPseudoPackages(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	writeMatchedRules(t, []*rule.InstRuleSet{
		{
			PackageName: "dep",
			ModulePath:  "example.com/app/dep",
			FuncRules: map[string][]*rule.InstFuncRule{
				"dep.go": {{InstBaseRule: rule.InstBaseRule{
					Name: "rule",
					Imports: map[string]string{
						"fmt":    "fmt",
						"unsafe": "unsafe",
						"cgo":    "C",
					},
				}}},
			},
		},
	})

	linkConfig := imports.ImportConfig{
		PackageFile: map[string]string{
			"context":             "/unused/context.a",
			"example.com/app/dep": "/unused/dep.a",
		},
	}
	updated, err := resolveRuleAddedImports(t.Context(), nil, &linkConfig)
	require.NoError(t, err)
	assert.True(t, updated)
	assert.Contains(t, linkConfig.PackageFile, "fmt")
	assert.NotContains(t, linkConfig.PackageFile, "unsafe")
	assert.NotContains(t, linkConfig.PackageFile, "C")
}

// TestRuleAddedImportsIgnoresMalformedMatchedRules checks that a matched.json
// the link cannot parse yields no candidates instead of failing the link.
func TestRuleAddedImportsIgnoresMalformedMatchedRules(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), []byte("{not json"), 0o600))

	linkConfig := imports.ImportConfig{PackageFile: map[string]string{"example.com/app/dep": "/unused/dep.a"}}
	assert.Empty(t, ruleAddedImports(t.Context(), nil, &linkConfig))
}

// TestFileRuleImportsSkipsUnusableSources checks every add_file source the
// link cannot use: no resolved path, a missing file, a malformed build
// constraint, and a file without a valid package clause.
func TestFileRuleImportsSkipsUnusableSources(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "badconstraint.go"),
		[]byte("//go:build (\n\npackage hooks\n\nimport \"os\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notgo.go"),
		[]byte("this is not Go\n"), 0o644))

	bctx := build.Default
	for _, tt := range []struct {
		name string
		rule *rule.InstFileRule
	}{
		{name: "no resolved path", rule: &rule.InstFileRule{File: "hooks.go"}},
		{name: "missing file", rule: &rule.InstFileRule{File: "missing.go", ResolvedPath: dir}},
		{name: "malformed build constraint", rule: &rule.InstFileRule{File: "badconstraint.go", ResolvedPath: dir}},
		{name: "no package clause", rule: &rule.InstFileRule{File: "notgo.go", ResolvedPath: dir}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Empty(t, fileRuleImports(t.Context(), &bctx, tt.rule))
		})
	}
}

// TestResolveRuleAddedImportsKeepsListedDependencies checks that a resolved
// transitive dependency the link already lists keeps its archive: only the
// missing packages are added.
func TestResolveRuleAddedImportsKeepsListedDependencies(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	writeMatchedRules(t, []*rule.InstRuleSet{{
		PackageName: "dep",
		ModulePath:  "example.com/app/dep",
		FuncRules: map[string][]*rule.InstFuncRule{
			"dep.go": {{InstBaseRule: rule.InstBaseRule{
				Name:    "rule",
				Target:  rule.NewTarget("example.com/app/dep"),
				Imports: map[string]string{"std": "fmt"},
			}}},
		},
	}})

	linkConfig := imports.ImportConfig{PackageFile: map[string]string{
		"example.com/app/dep": "/unused/dep.a",
		"errors":              "/listed/errors.a",
	}}
	updated, err := resolveRuleAddedImports(t.Context(), nil, &linkConfig)
	require.NoError(t, err)
	assert.True(t, updated)
	assert.Contains(t, linkConfig.PackageFile, "fmt")
	assert.Equal(t, "/listed/errors.a", linkConfig.PackageFile["errors"],
		"a dependency the link already lists must keep its archive")
}

// TestResolveRuleAddedImportsFollowsChainedRuleSets covers recovery when a
// rule-added import is itself the target of a matched rule set: a rule adds
// fmt to dep, and a matched rule on fmt adds encoding/xml. The link starts
// without fmt, so the first pass cannot even see fmt's rule set; the pass
// after fmt joins the importcfg must resolve encoding/xml. encoding/xml is
// not a dependency of fmt, so only the chain, not a transitive resolution,
// can add it.
func TestResolveRuleAddedImportsFollowsChainedRuleSets(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	writeMatchedRules(t, []*rule.InstRuleSet{
		{
			PackageName: "dep",
			ModulePath:  "example.com/app/dep",
			FuncRules: map[string][]*rule.InstFuncRule{
				"dep.go": {{InstBaseRule: rule.InstBaseRule{
					Name:    "dep-rule",
					Target:  rule.NewTarget("example.com/app/dep"),
					Imports: map[string]string{"fmt": "fmt"},
				}}},
			},
		},
		{
			PackageName: "fmt",
			ModulePath:  "fmt",
			FuncRules: map[string][]*rule.InstFuncRule{
				"print.go": {{InstBaseRule: rule.InstBaseRule{
					Name:    "fmt-rule",
					Target:  rule.NewTarget("fmt"),
					Imports: map[string]string{"xml": "encoding/xml"},
				}}},
			},
		},
	})

	// The link lists only the rule target itself; every rule-added import is
	// missing and only the recovery can supply it.
	linkConfig := imports.ImportConfig{PackageFile: map[string]string{
		"example.com/app/dep": "/unused/dep.a",
	}}
	updated, err := resolveRuleAddedImports(t.Context(), nil, &linkConfig)
	require.NoError(t, err)
	assert.True(t, updated)
	assert.Contains(t, linkConfig.PackageFile, "fmt",
		"the first pass must resolve the rule-added import itself")
	assert.Contains(t, linkConfig.PackageFile, "encoding/xml",
		"the pass after fmt joins the importcfg must resolve the rule set fmt carries")
}

// TestResolveRuleAddedImportsKeepsLinkedBinarysMainSets covers a
// multi-binary build: every main package compiles with -p main, so every
// executable's rule set carries ModulePath "main" and no link importcfg can
// name the package a set instrumented. The link -o output name can: go
// names a binary after the last element of its main package's import path,
// which is also the directory the set's matched rules name. A custom -o
// name matches no set, so every main set stays and recovery never drops an
// import the link needs.
func TestResolveRuleAddedImportsKeepsLinkedBinarysMainSets(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	mainSet := func(name, dir string, imports map[string]string) *rule.InstRuleSet {
		set := rule.NewInstRuleSet("main")
		set.SetPackageName("main")
		set.AddFuncRule(filepath.Join(workDir, dir, "main.go"), &rule.InstFuncRule{
			InstBaseRule: rule.InstBaseRule{
				Name:    name,
				Target:  rule.NewTarget("main"),
				Imports: imports,
			},
			Func: "main",
		})
		return set
	}
	writeMatchedRules(t, []*rule.InstRuleSet{
		mainSet("rule-a", "a", map[string]string{"fmt": "fmt"}),
		mainSet("rule-b", "b", map[string]string{"strings": "strings"}),
	})

	// The link importcfg names no main package by path, the shape a warm
	// multi-binary build gives every link.
	newLinkConfig := func() *imports.ImportConfig {
		return &imports.ImportConfig{PackageFile: map[string]string{
			"runtime": "/unused/runtime.a",
		}}
	}
	linkCmd := func(binary string) []string {
		return []string{"link", "-o", filepath.Join(workDir, binary), "-buildid=test", "-importcfg", "unused"}
	}

	// fmt resolves no strings in its dependency closure, so only the kept
	// main set can add strings.
	t.Run("keeps only the linked binary's main set", func(t *testing.T) {
		linkConfig := newLinkConfig()
		updated, err := resolveRuleAddedImports(t.Context(), linkCmd("a"), linkConfig)
		require.NoError(t, err)
		assert.True(t, updated)
		assert.Contains(t, linkConfig.PackageFile, "fmt")
		assert.NotContains(t, linkConfig.PackageFile, "strings",
			"the link builds the a binary, so the b binary's main rule set must not contribute")
	})

	t.Run("custom -o name keeps every main set", func(t *testing.T) {
		linkConfig := newLinkConfig()
		updated, err := resolveRuleAddedImports(t.Context(), linkCmd("custom-name"), linkConfig)
		require.NoError(t, err)
		assert.True(t, updated)
		assert.Contains(t, linkConfig.PackageFile, "fmt")
		assert.Contains(t, linkConfig.PackageFile, "strings",
			"a custom -o name matches no main set, so no set may be dropped")
	})
}

// TestInterceptLinkFailsWhenRecoveryFails checks that an error while
// recovering rule-added imports stops the link: an unreadable recorded PGO
// profile and a nested go list that cannot run both surface as errors.
func TestInterceptLinkFailsWhenRecoveryFails(t *testing.T) {
	sets := []*rule.InstRuleSet{{
		PackageName: "dep",
		ModulePath:  "example.com/app/dep",
		FuncRules: map[string][]*rule.InstFuncRule{
			"dep.go": {{InstBaseRule: rule.InstBaseRule{
				Name:    "rule",
				Target:  rule.NewTarget("example.com/app/dep"),
				Imports: map[string]string{"std": "fmt"},
			}}},
		},
	}}
	writeLinkConfig := func(t *testing.T, workDir string) string {
		t.Helper()
		cfgPath := filepath.Join(workDir, "importcfg.link")
		require.NoError(t, os.WriteFile(cfgPath, []byte("packagefile example.com/app/dep=/unused/dep.a\n"), 0o644))
		return cfgPath
	}

	t.Run("unreadable recorded PGO profile", func(t *testing.T) {
		workDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, workDir)
		writeMatchedRules(t, sets)
		// A directory where the profile record belongs makes the read fail.
		require.NoError(t, os.MkdirAll(util.GetPGOProfileFile(), 0o755))

		// pgoBuildFlagsFor reads the recorded profile only for a real link
		// tool path, so this case uses one.
		args := linkArgs(writeLinkConfig(t, workDir))
		args[0] = linkToolPath()
		_, err := interceptLink(t.Context(), args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "PGO profile")
	})

	t.Run("nested go list cannot run", func(t *testing.T) {
		workDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, workDir)
		t.Setenv(util.EnvOtelcBuildFlags, `["-otelc-not-a-go-flag"]`)
		writeMatchedRules(t, sets)

		_, err := interceptLink(t.Context(), linkArgs(writeLinkConfig(t, workDir)))
		require.Error(t, err)
	})
}

// linkToolPath returns a path that util.IsLinkCommandWithArgs accepts as the
// link tool.
func linkToolPath() string {
	name := "link"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(string(filepath.Separator)+"go", "pkg", "tool", name)
}
