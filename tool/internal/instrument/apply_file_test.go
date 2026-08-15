// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"io"
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

	ip := &InstrumentPhase{
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
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

	ip := &InstrumentPhase{
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
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

	ip := &InstrumentPhase{
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
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

	ip := &InstrumentPhase{
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
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
