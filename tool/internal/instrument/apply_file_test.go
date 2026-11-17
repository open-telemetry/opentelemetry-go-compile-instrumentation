// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"errors"
	"go/build"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
	toolruntime "go.opentelemetry.io/otelc/tool/internal/runtime"
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

func TestApplyFileRule_InternalFile(t *testing.T) {
	workDir := t.TempDir()
	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}

	err := ip.applyFileRule(t.Context(), toolruntime.FileRule(), "runtime")
	require.NoError(t, err)

	outPath := filepath.Join(workDir, "otelc.runtime_gls.go")
	require.FileExists(t, outPath)
	assert.Contains(t, ip.compileArgs, outPath)

	outData, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(outData), "package runtime")
	assert.Contains(t, string(outData), "GetTraceContextFromGLS")

	unknownRule := &rule.InstFileRule{File: "missing.go", Path: util.InternalPath}
	err = ip.applyFileRule(t.Context(), unknownRule, "runtime")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown internal runtime file")
}

func skipIfSymlinkUnsupported(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if errors.Is(err, errors.ErrUnsupported) ||
		errors.Is(err, os.ErrPermission) ||
		strings.Contains(err.Error(), "privilege") ||
		strings.Contains(err.Error(), "operation not supported") ||
		strings.Contains(err.Error(), "not implemented") {
		t.Skipf("skipping test: symlinks not supported or permitted in this environment: %v", err)
	}
	require.NoError(t, err)
}

func TestApplyFileRule_ParentTraversalRejected(t *testing.T) {
	parentDir := t.TempDir()
	workDir := t.TempDir()

	srcDir := filepath.Join(parentDir, "pkg")
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755))

	// Create an actual outside file to ensure traversal tests don't pass merely because the file is missing.
	outsideSecret := filepath.Join(parentDir, "secret.go")
	require.NoError(t, os.WriteFile(outsideSecret, []byte("package outside\nfunc Secret() {}\n"), 0o644))

	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}

	tests := []struct {
		name         string
		file         string
		resolvedPath string
	}{
		{
			name:         "single level traversal",
			file:         "../secret.go",
			resolvedPath: srcDir,
		},
		{
			name:         "multi-level traversal",
			file:         "../../secret.go",
			resolvedPath: filepath.Join(srcDir, "sub"),
		},
		{
			name:         "nested traversal escaping base",
			file:         "sub/../../secret.go",
			resolvedPath: srcDir,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Exercise the read boundary directly, bypassing constructor validation
			fileRule := &rule.InstFileRule{
				File:         tt.file,
				Path:         "example.com/mypkg",
				ResolvedPath: tt.resolvedPath,
			}
			fileRule.Name = "test_traversal_rule"

			err := ip.applyFileRule(t.Context(), fileRule, "targetpkg")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "is outside rule package directory")
		})
	}
}

func TestApplyFileRule_AbsolutePathRejected(t *testing.T) {
	srcDir := t.TempDir()
	outsideDir := t.TempDir()
	workDir := t.TempDir()

	outsideSecret := filepath.Join(outsideDir, "secret.go")
	require.NoError(t, os.WriteFile(outsideSecret, []byte("package outside\nfunc Secret() {}\n"), 0o644))

	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}

	tests := []struct {
		name string
		file string
	}{
		{
			name: "posix or os absolute path",
			file: outsideSecret,
		},
		{
			name: "windows drive-absolute path",
			file: `C:\outside\secret.go`,
		},
		{
			name: "windows rooted path",
			file: `\outside\secret.go`,
		},
		{
			name: "windows drive-relative path",
			file: `C:secret.go`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Exercise the read boundary directly, bypassing constructor validation
			fileRule := &rule.InstFileRule{
				File:         tt.file,
				Path:         "example.com/mypkg",
				ResolvedPath: srcDir,
			}
			fileRule.Name = "test_abs_rule"

			err := ip.applyFileRule(t.Context(), fileRule, "targetpkg")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must be a relative path")
		})
	}
}

func TestApplyFileRule_SiblingDirectoryPrefixRejected(t *testing.T) {
	parentDir := t.TempDir()
	workDir := t.TempDir()

	srcDir := filepath.Join(parentDir, "pkg")
	siblingDir := filepath.Join(parentDir, "pkg-sibling")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.MkdirAll(siblingDir, 0o755))

	// An existing file in a sibling directory whose path begins with the same string prefix (".../pkg")
	outsideSecret := filepath.Join(siblingDir, "secret.go")
	require.NoError(t, os.WriteFile(outsideSecret, []byte("package sibling\nfunc Secret() {}\n"), 0o644))

	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}

	// Exercise read boundary directly with a path targeting the sibling directory
	fileRule := &rule.InstFileRule{
		File:         "../pkg-sibling/secret.go",
		Path:         "example.com/mypkg",
		ResolvedPath: srcDir,
	}
	fileRule.Name = "test_sibling_prefix_rule"

	err := ip.applyFileRule(t.Context(), fileRule, "targetpkg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is outside rule package directory")
}

func TestApplyFileRule_SymlinkedBaseDirectory_Success(t *testing.T) {
	realSrcDir := t.TempDir()
	tempParent := t.TempDir()
	workDir := t.TempDir()

	content := `//go:build ignore

package sourcepkg

func Helper() string {
	return "ok"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(realSrcDir, "helper.go"), []byte(content), 0o644))

	symlinkBase := filepath.Join(tempParent, "symlink_pkg")
	err := os.Symlink(realSrcDir, symlinkBase)
	skipIfSymlinkUnsupported(t, err)

	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}

	fileRule := &rule.InstFileRule{
		File:         "helper.go",
		Path:         "example.com/mypkg",
		ResolvedPath: symlinkBase,
	}
	fileRule.Name = "test_symlinked_base"

	err = ip.applyFileRule(t.Context(), fileRule, "targetpkg")
	require.NoError(t, err)

	outPath := filepath.Join(workDir, "otelc.helper.go")
	require.FileExists(t, outPath)

	outData, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(outData), "package targetpkg")
	assert.Contains(t, string(outData), "func Helper()")
}

func TestApplyFileRule_SymlinkEscapeRejected(t *testing.T) {
	srcDir := t.TempDir()
	outsideDir := t.TempDir()
	workDir := t.TempDir()

	outsideSecret := filepath.Join(outsideDir, "secret.go")
	require.NoError(t, os.WriteFile(outsideSecret, []byte("package outside\nfunc Secret() {}\n"), 0o644))

	// Create a symlink inside srcDir that points outside srcDir
	linkPath := filepath.Join(srcDir, "link_to_secret.go")
	err := os.Symlink(outsideSecret, linkPath)
	skipIfSymlinkUnsupported(t, err)

	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}

	// Lexically "link_to_secret.go" appears inside srcDir, but resolves outside
	fileRule := &rule.InstFileRule{
		File:         "link_to_secret.go",
		Path:         "example.com/mypkg",
		ResolvedPath: srcDir,
	}
	fileRule.Name = "test_symlink_escape"

	err = ip.applyFileRule(t.Context(), fileRule, "targetpkg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading rule source file")
	assert.NotContains(t, err.Error(), "not found in")
}

func TestApplyFileRule_SymlinkInsideSucceeds(t *testing.T) {
	srcDir := t.TempDir()
	workDir := t.TempDir()

	content := `//go:build ignore

package sourcepkg

func Target() string {
	return "ok"
}
`
	targetFile := filepath.Join(srcDir, "target.go")
	require.NoError(t, os.WriteFile(targetFile, []byte(content), 0o644))

	// Create a relative symlink inside srcDir that points to a target inside srcDir.
	// Root access permits contained relative symlinks, but not absolute targets.
	linkPath := filepath.Join(srcDir, "link_target.go")
	err := os.Symlink("target.go", linkPath)
	skipIfSymlinkUnsupported(t, err)

	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}

	fileRule := &rule.InstFileRule{
		File:         "link_target.go",
		Path:         "example.com/mypkg",
		ResolvedPath: srcDir,
	}
	fileRule.Name = "test_symlink_inside"

	err = ip.applyFileRule(t.Context(), fileRule, "targetpkg")
	require.NoError(t, err)

	outPath := filepath.Join(workDir, "otelc.link_target.go")
	require.FileExists(t, outPath)

	outData, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(outData), "package targetpkg")
	assert.Contains(t, string(outData), "func Target()")
}

func TestApplyFileRule_DirectorySymlinkEscapeRejected(t *testing.T) {
	parentDir := t.TempDir()
	srcDir := filepath.Join(parentDir, "src")
	outsideDir := filepath.Join(parentDir, "outside")
	workDir := t.TempDir()

	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.MkdirAll(outsideDir, 0o755))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(outsideDir, "secret.go"), []byte("package outside\nfunc Secret() {}\n"), 0o644),
	)

	linkPath := filepath.Join(srcDir, "outside")
	err := os.Symlink(filepath.Join("..", "outside"), linkPath)
	skipIfSymlinkUnsupported(t, err)

	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}
	fileRule := &rule.InstFileRule{
		File:         "outside/secret.go",
		Path:         "example.com/mypkg",
		ResolvedPath: srcDir,
	}
	fileRule.Name = "test_directory_symlink_escape"

	err = ip.applyFileRule(t.Context(), fileRule, "targetpkg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading rule source file")
	assert.NotContains(t, err.Error(), "not found in")
}

func TestApplyFileRule_RelativeDirectorySymlinkInsideSucceeds(t *testing.T) {
	srcDir := t.TempDir()
	workDir := t.TempDir()

	internalDir := filepath.Join(srcDir, "internal")
	require.NoError(t, os.MkdirAll(internalDir, 0o755))
	content := `package sourcepkg

func Target() string {
	return "ok"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(internalDir, "helper.go"), []byte(content), 0o644))

	linkPath := filepath.Join(srcDir, "alias")
	err := os.Symlink("internal", linkPath)
	skipIfSymlinkUnsupported(t, err)

	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}
	fileRule := &rule.InstFileRule{
		File:         "alias/helper.go",
		Path:         "example.com/mypkg",
		ResolvedPath: srcDir,
	}
	fileRule.Name = "test_relative_directory_symlink_inside"

	err = ip.applyFileRule(t.Context(), fileRule, "targetpkg")
	require.NoError(t, err)

	outPath := filepath.Join(workDir, "otelc.helper.go")
	require.FileExists(t, outPath)

	outData, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(outData), "package targetpkg")
	assert.Contains(t, string(outData), "func Target()")
}

func TestApplyFileRule_SameBaseNameFromDifferentDirs(t *testing.T) {
	workDir := t.TempDir()
	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}

	funcs := []string{"One", "Two", "Three"}
	for _, fn := range funcs {
		srcDir := t.TempDir()
		content := "package sourcepkg\n\nfunc " + fn + "() {}\n"
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "setup.go"), []byte(content), 0o644))
		fileRule := &rule.InstFileRule{File: "setup.go", Path: "example.com/" + fn, ResolvedPath: srcDir}
		fileRule.Name = "add_" + fn
		require.NoError(t, ip.applyFileRule(t.Context(), fileRule, "targetpkg"))
	}

	want := []string{
		filepath.Join(workDir, "otelc.setup.go"),
		filepath.Join(workDir, "otelc.setup.1.go"),
		filepath.Join(workDir, "otelc.setup.2.go"),
	}
	assert.Equal(t, want, ip.compileArgs)
	for i, path := range want {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(data), "func "+funcs[i]+"()")
	}
}

func TestApplyFileRule_DoesNotTakeGlobalsFileName(t *testing.T) {
	srcDir := t.TempDir()
	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(srcDir, "globals.go"),
		[]byte("package sourcepkg\n\nfunc Greeting() string { return \"hi\" }\n"),
		0o644,
	))

	ip := &instrumentPhase{
		logger:  slog.New(slog.DiscardHandler),
		workDir: workDir,
	}
	fileRule := &rule.InstFileRule{File: "globals.go", Path: "example.com/mypkg", ResolvedPath: srcDir}
	fileRule.Name = "add_globals"
	require.NoError(t, ip.applyFileRule(t.Context(), fileRule, "targetpkg"))

	outPath := filepath.Join(workDir, "otelc.globals.1.go")
	assert.Equal(t, []string{outPath}, ip.compileArgs)
	assert.NoFileExists(t, filepath.Join(workDir, otelcGlobalsFile))

	// The globals file written afterwards must not replace the added file.
	require.NoError(t, ip.writeGlobals("targetpkg"))
	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "func Greeting()")
	assert.Equal(t, []string{outPath, filepath.Join(workDir, otelcGlobalsFile)}, ip.compileArgs)
}
