// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/build"
	"go/build/constraint"
	"log/slog"
	"os"
	"path/filepath"
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
		{
			name: "strips build ignore while preserving platform tag in combined directive",
			input: `//go:build ignore && linux

package main

func main() {}
`,
			expected: `//go:build linux

package main

func main() {}
`,
		},
		{
			name: "strips build ignore while preserving platform tag in reversed combined directive",
			input: `//go:build linux && ignore

package main

func main() {}
`,
			expected: `//go:build linux

package main

func main() {}
`,
		},
		{
			name: "preserves standalone platform directive without ignore",
			input: `//go:build linux

package main

func main() {}
`,
			expected: `//go:build linux

package main

func main() {}
`,
		},
		{
			name: "strips legacy plus:build ignore directive",
			input: `// +build ignore

package main

func main() {}
`,
			expected: `

package main

func main() {}
`,
		},
		{
			name: "strips legacy plus:build ignore directive while preserving legacy platform directive",
			input: `// +build ignore
// +build darwin linux

package main

func main() {}
`,
			expected: `
// +build darwin linux

package main

func main() {}
`,
		},
		{
			name: "strips legacy plus:build ignore while preserving legacy combined directive with comma",
			input: `// +build ignore,linux

package main

func main() {}
`,
			expected: `// +build linux

package main

func main() {}
`,
		},
		{
			name: "strips legacy plus:build ignore while preserving legacy combined directive with space",
			input: `// +build ignore linux

package main

func main() {}
`,
			expected: `// +build linux

package main

func main() {}
`,
		},
		{
			name: "strips ignore from compound modern build directive containing ignore",
			input: `//go:build ignore && (darwin || linux)

package main

func main() {}
`,
			expected: `//go:build darwin || linux

package main

func main() {}
`,
		},
		{
			name: "preserves compound modern build directive without ignore",
			input: `//go:build darwin || linux

package main

func main() {}
`,
			expected: `//go:build darwin || linux

package main

func main() {}
`,
		},
		{
			name: "malformed build directive is not treated as ignore and left unchanged",
			input: `//go:build (invalid &&
package main
`,
			expected: `//go:build (invalid &&
package main
`,
		},
		{
			name: "strips ignore from compound directive with negated platform tag and ignore",
			input: `//go:build !windows && ignore

package main

func main() {}
`,
			expected: `//go:build !windows

package main

func main() {}
`,
		},
		{
			name: "preserves directive with negated platform tag without ignore",
			input: `//go:build !windows

package main

func main() {}
`,
			expected: `//go:build !windows

package main

func main() {}
`,
		},
		{
			name: "strips directive with negated ignore tag",
			input: `//go:build !ignore

package main

func main() {}
`,
			expected: `

package main

func main() {}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripBuildIgnoreTag(tt.input))
		})
	}
}

type dummyConstraintExpr struct {
	constraint.Expr
}

func TestRemoveIgnore_Fallback(t *testing.T) {
	dummy := &dummyConstraintExpr{}
	assert.Equal(t, dummy, removeIgnore(dummy))
	assert.Nil(t, removeIgnore(nil))
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
