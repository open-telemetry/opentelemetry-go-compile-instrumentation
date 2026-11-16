// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func TestStripBuildIgnoreTag(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "strips go:build ignore comment",
			input: `//go:build ignore

package main

func main() {}
`,
			expected: `

package main

func main() {}
`,
		},
		{
			name: "no go:build ignore comment - content unchanged",
			input: `package main

func main() {}
`,
			expected: `package main

func main() {}
`,
		},
		{
			name: "multiple go:build ignore comments",
			input: `//go:build ignore
package main
//go:build ignore
func main() {}
`,
			expected: `
package main

func main() {}
`,
		},
		{
			name:     "empty file",
			input:    "",
			expected: "",
		},
		{
			// Regression test for #1069: a whole-file substring replace
			// deleted this text from the string literal and the comment,
			// since both happen to contain "//go:build ignore" without being
			// a build-constraint line themselves.
			name: "preserves the tag text inside a string literal and comment prose",
			input: `//go:build ignore

package hooks

// Every file rule source must carry //go:build ignore at the top.
const marker = "//go:build ignore"
`,
			expected: `

package hooks

// Every file rule source must carry //go:build ignore at the top.
const marker = "//go:build ignore"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripBuildIgnoreTag(tt.input))
		})
	}
}

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
