// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
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
			name: "strips combined build ignore and platform directive",
			input: `//go:build ignore && linux

package main

func main() {}
`,
			expected: `

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
			name: "strips compound modern build directive containing ignore",
			input: `//go:build ignore && (darwin || linux)

package main

func main() {}
`,
			expected: `

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

// TestApplyFileRule_CombinedIgnoreAndPlatformTag verifies that a file rule source
// carrying the combined constraint //go:build ignore && linux has the entire
// constraint line stripped. Prior to the containsIgnoreTag fix, expr.String()
// returned "ignore && linux" (not "ignore"), so the line was left in the generated
// source, which would make the injected file invisible to the compiler.
//
// NOTE on compile-time platform enforcement: applyFileRule currently adds the
// generated file unconditionally to compileArgs regardless of GOOS/GOARCH.
// Evaluating any remaining build constraints against the target platform and
// conditionally omitting the file from the compile command is a broader follow-up
// (see the discussion on PR #1358).
func TestApplyFileRule_CombinedIgnoreAndPlatformTag(t *testing.T) {
	srcDir := t.TempDir()
	workDir := t.TempDir()

	// //go:build ignore && linux is valid Go: a single compound expression.
	// The whole line must be stripped because it contains the "ignore" tag.
	content := "//go:build ignore && linux\n\npackage sourcepkg\n\nfunc LinuxHelper() string {\n\treturn \"linux\"\n}\n"
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
	fileRule.Name = "test_linux_combined_rule"

	err := ip.applyFileRule(t.Context(), fileRule, "targetpkg")
	require.NoError(t, err)

	outPath := filepath.Join(workDir, "otelc.helper.go")
	require.FileExists(t, outPath)

	outData, err := os.ReadFile(outPath)
	require.NoError(t, err)
	// The entire //go:build ignore && linux line must be gone — not just the
	// ignore term — because the line-level stripping removes the whole directive.
	assert.NotContains(t, string(outData), "//go:build ignore && linux")
	assert.NotContains(t, string(outData), "//go:build ignore")
	// The generated file is still added to compileArgs by applyFileRule;
	// restricting it to the matching platform is a follow-up concern.
	assert.Contains(t, ip.compileArgs, outPath)
	assert.Contains(t, string(outData), "package targetpkg")
	assert.Contains(t, string(outData), "func LinuxHelper()")
}
