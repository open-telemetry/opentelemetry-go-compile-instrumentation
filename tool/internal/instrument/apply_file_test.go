// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/build"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestApplyFileRule_Success(t *testing.T) {
	srcDir := t.TempDir()
	workDir := t.TempDir()

	content := `//go:build ignore

package sourcepkg

func Helper() string {
	return "ok"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "helper.go"), []byte(content), 0o644))

	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}

	fileRule := &rule.InstFileRule{
		File:         "helper.go",
		Path:         "example.com/mypkg",
		ResolvedPath: srcDir,
	}
	fileRule.Name = "test_file_rule"

	err := ip.applyFileRule(t.Context(), fileRule, "targetpkg")
	require.NoError(t, err)

	outPath := filepath.Join(workDir, "otelc.helper.go")
	require.FileExists(t, outPath)

	outData, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(outData), "package targetpkg")
	assert.Contains(t, string(outData), "func Helper()")
	assert.NotContains(t, string(outData), "//go:build ignore")
	assert.Contains(t, ip.compileArgs, outPath)
}

func TestApplyFileRule_NoCollisionWithSuffix(t *testing.T) {
	srcDir := t.TempDir()
	workDir := t.TempDir()

	customContent := `package sourcepkg
func CustomHelper() {}
`
	origContent := `package sourcepkg
func OrigHelper() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "custom_helper.go"), []byte(customContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "helper.go"), []byte(origContent), 0o644))

	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}

	fileRule := &rule.InstFileRule{
		File:         "helper.go",
		Path:         "example.com/mypkg",
		ResolvedPath: srcDir,
	}
	fileRule.Name = "test_file_rule"

	err := ip.applyFileRule(t.Context(), fileRule, "targetpkg")
	require.NoError(t, err)

	outPath := filepath.Join(workDir, "otelc.helper.go")
	require.FileExists(t, outPath)

	outData, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(outData), "OrigHelper")
	assert.NotContains(t, string(outData), "CustomHelper")
}

func TestApplyFileRule_FileNotFound(t *testing.T) {
	srcDir := t.TempDir()
	workDir := t.TempDir()

	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}

	fileRule := &rule.InstFileRule{
		File:         "nonexistent.go",
		Path:         "example.com/mypkg",
		ResolvedPath: srcDir,
	}
	fileRule.Name = "test_missing_file_rule"

	err := ip.applyFileRule(t.Context(), fileRule, "targetpkg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file nonexistent.go not found in")
}

func TestApplyFileRule_EmptyPackageName(t *testing.T) {
	srcDir := t.TempDir()
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(srcDir, "added.go"),
		[]byte("package myrules\n\nfunc init() {}\n"),
		0o644,
	))

	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}

	fileRule := &rule.InstFileRule{
		File:         "added.go",
		Path:         "example.com/myrules",
		ResolvedPath: srcDir,
	}
	fileRule.Name = "test_empty_package_name"

	err := ip.applyFileRule(t.Context(), fileRule, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve the package name")

	entries, readErr := os.ReadDir(workDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries)
	assert.Empty(t, ip.compileArgs)
}

func TestApplyFileRule_SubdirectoryFile(t *testing.T) {
	srcDir := t.TempDir()
	workDir := t.TempDir()

	subDir := filepath.Join(srcDir, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	content := `package sourcepkg
func SubHelper() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "helper.go"), []byte(content), 0o644))

	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}

	fileRule := &rule.InstFileRule{
		File:         "sub/helper.go",
		Path:         "example.com/mypkg",
		ResolvedPath: srcDir,
	}
	fileRule.Name = "test_sub_file_rule"

	err := ip.applyFileRule(t.Context(), fileRule, "targetpkg")
	require.NoError(t, err)

	outPath := filepath.Join(workDir, "otelc.helper.go")
	require.FileExists(t, outPath)

	outData, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(outData), "package targetpkg")
	assert.Contains(t, string(outData), "func SubHelper()")
}

// TestApplyFileRule_CombinedIgnoreAndPlatformTag verifies that a file rule source
// carrying the combined constraint //go:build ignore && linux has the "ignore" tag
// stripped while preserving //go:build linux in the output file, and that it is
// included in compileArgs when compiling for linux.
func TestApplyFileRule_CombinedIgnoreAndPlatformTag(t *testing.T) {
	srcDir := t.TempDir()
	workDir := t.TempDir()

	content := "//go:build ignore && linux\n\npackage sourcepkg\n\nfunc LinuxHelper() string {\n\treturn \"linux\"\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "helper.go"), []byte(content), 0o644))

	ip := &instrumentPhase{
		logger:       slog.New(slog.DiscardHandler),
		workDir:      workDir,
		buildContext: &build.Context{GOOS: "linux", GOARCH: "amd64"},
	}

	fileRule := &rule.InstFileRule{
		File:         "helper.go",
		Path:         "example.com/mypkg",
		ResolvedPath: srcDir,
	}
	fileRule.Name = "test_linux_combined_rule"

	err := ip.applyFileRule(t.Context(), fileRule, "targetpkg")
	require.NoError(t, err)

	outPath := filepath.Join(workDir, "otelc.helper.go")
	require.FileExists(t, outPath)

	outData, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(outData), "//go:build linux")
	assert.NotContains(t, string(outData), "ignore")
	assert.Contains(t, ip.compileArgs, outPath)
	assert.Contains(t, string(outData), "package targetpkg")
	assert.Contains(t, string(outData), "func LinuxHelper()")
}

// TestApplyFileRule_PlatformConstraintMatching verifies that applyFileRule respects
// build constraints at compile time:
// - A file matching the target platform is generated and added to compileArgs.
// - A file for a non-matching platform is skipped and NOT added to compileArgs.
// - Filename conventions (e.g. helper_linux.go) are also honored.
func TestApplyFileRule_PlatformConstraintMatching(t *testing.T) {
	t.Run("skips file when platform constraint does not match target GOOS", func(t *testing.T) {
		srcDir := t.TempDir()
		workDir := t.TempDir()

		content := "//go:build ignore && linux\n\npackage sourcepkg\n\nfunc LinuxOnly() {}\n"
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "helper.go"), []byte(content), 0o644))

		ip := &instrumentPhase{
			logger:       slog.New(slog.DiscardHandler),
			workDir:      workDir,
			buildContext: &build.Context{GOOS: "windows", GOARCH: "amd64"},
		}

		fileRule := &rule.InstFileRule{
			File:         "helper.go",
			Path:         "example.com/mypkg",
			ResolvedPath: srcDir,
		}
		fileRule.Name = "test_linux_on_windows"

		err := ip.applyFileRule(t.Context(), fileRule, "targetpkg")
		require.NoError(t, err)

		outPath := filepath.Join(workDir, "otelc.helper.go")
		assert.NoFileExists(t, outPath)
		assert.NotContains(t, ip.compileArgs, outPath)
	})

	t.Run("includes file when platform constraint matches target GOOS", func(t *testing.T) {
		srcDir := t.TempDir()
		workDir := t.TempDir()

		content := "//go:build ignore && (darwin || linux)\n\npackage sourcepkg\n\nfunc UnixHelper() {}\n"
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "helper.go"), []byte(content), 0o644))

		ip := &instrumentPhase{
			logger:       slog.New(slog.DiscardHandler),
			workDir:      workDir,
			buildContext: &build.Context{GOOS: "darwin", GOARCH: "arm64"},
		}

		fileRule := &rule.InstFileRule{
			File:         "helper.go",
			Path:         "example.com/mypkg",
			ResolvedPath: srcDir,
		}
		fileRule.Name = "test_unix_on_darwin"

		err := ip.applyFileRule(t.Context(), fileRule, "targetpkg")
		require.NoError(t, err)

		outPath := filepath.Join(workDir, "otelc.helper.go")
		require.FileExists(t, outPath)
		assert.Contains(t, ip.compileArgs, outPath)

		outData, err := os.ReadFile(outPath)
		require.NoError(t, err)
		assert.Contains(t, string(outData), "//go:build darwin || linux")
		assert.NotContains(t, string(outData), "ignore")
	})

	t.Run("skips file when filename suffix does not match target GOOS", func(t *testing.T) {
		srcDir := t.TempDir()
		workDir := t.TempDir()

		content := "//go:build ignore\n\npackage sourcepkg\n\nfunc LinuxSuffix() {}\n"
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "helper_linux.go"), []byte(content), 0o644))

		ip := &instrumentPhase{
			logger:       slog.New(slog.DiscardHandler),
			workDir:      workDir,
			buildContext: &build.Context{GOOS: "windows", GOARCH: "amd64"},
		}

		fileRule := &rule.InstFileRule{
			File:         "helper_linux.go",
			Path:         "example.com/mypkg",
			ResolvedPath: srcDir,
		}
		fileRule.Name = "test_linux_suffix_on_windows"

		err := ip.applyFileRule(t.Context(), fileRule, "targetpkg")
		require.NoError(t, err)

		outPath := filepath.Join(workDir, "otelc.helper_linux.go")
		assert.NoFileExists(t, outPath)
		assert.NotContains(t, ip.compileArgs, outPath)
	})
}

func TestApplyFileRule_InvalidBuildConstraint(t *testing.T) {
	srcDir := t.TempDir()
	content := "//go:build linux &&\n\npackage sourcepkg\n"
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "helper.go"), []byte(content), 0o644))

	ip := &instrumentPhase{
		logger:       slog.New(slog.DiscardHandler),
		workDir:      t.TempDir(),
		buildContext: &build.Context{GOOS: "linux", GOARCH: "amd64"},
	}
	fileRule := &rule.InstFileRule{File: "helper.go", Path: "example.com/mypkg", ResolvedPath: srcDir}
	fileRule.Name = "test_invalid_constraint"

	err := ip.applyFileRule(t.Context(), fileRule, "targetpkg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "matching build constraints")
}

func TestApplyFileRule_UsesForwardedBuildTags(t *testing.T) {
	content := "//go:build ignore && enterprise\n\npackage sourcepkg\n\nfunc EnterpriseOnly() {}\n"

	run := func(t *testing.T, buildFlags []string) (string, *instrumentPhase) {
		t.Helper()
		t.Setenv("GOFLAGS", "")
		t.Setenv(util.EnvOtelcBuildFlags, util.EncodeBuildFlags(buildFlags))

		srcDir := t.TempDir()
		workDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "helper.go"), []byte(content), 0o644))

		ip := &instrumentPhase{logger: slog.New(slog.DiscardHandler), workDir: workDir}
		fileRule := &rule.InstFileRule{File: "helper.go", Path: "example.com/mypkg", ResolvedPath: srcDir}
		fileRule.Name = "test_enterprise_tag"
		require.NoError(t, ip.applyFileRule(t.Context(), fileRule, "targetpkg"))
		return filepath.Join(workDir, "otelc.helper.go"), ip
	}

	t.Run("includes file when the build passes the tag", func(t *testing.T) {
		outPath, ip := run(t, []string{"-tags=enterprise"})
		assert.FileExists(t, outPath)
		assert.Contains(t, ip.compileArgs, outPath)
	})

	t.Run("skips file when the build does not pass the tag", func(t *testing.T) {
		outPath, ip := run(t, nil)
		assert.NoFileExists(t, outPath)
		assert.NotContains(t, ip.compileArgs, outPath)
	})
}

func TestApplyFileRule_DiffUnderDebug(t *testing.T) {
	srcDir := t.TempDir()
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	content := `package sourcepkg

func Helper() string {
	return "ok"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "helper.go"), []byte(content), 0o644))

	ip := &instrumentPhase{
		logger:      slog.New(slog.DiscardHandler),
		workDir:     workDir,
		compileArgs: []string{"-p", "example.com/mypkg"},
	}

	fileRule := &rule.InstFileRule{
		File:         "helper.go",
		Path:         "example.com/mypkg",
		ResolvedPath: srcDir,
	}
	fileRule.Name = "test_file_rule"

	err := ip.applyFileRule(t.Context(), fileRule, "targetpkg")
	require.NoError(t, err)

	outPath := filepath.Join(workDir, "otelc.helper.go")
	require.FileExists(t, outPath)
	assert.Contains(t, ip.compileArgs, outPath)

	diffPath := filepath.Join(ip.debugArtifactDir(), "otelc.helper.go.diff")
	require.FileExists(t, diffPath)
	diffBytes, err := os.ReadFile(diffPath)
	require.NoError(t, err)
	diffText := string(diffBytes)
	assert.Contains(t, diffText, "=== rule: test_file_rule ===")
	assert.Contains(t, diffText, "--- /dev/null")
	assert.Contains(t, diffText, "+++ "+outPath)
}
