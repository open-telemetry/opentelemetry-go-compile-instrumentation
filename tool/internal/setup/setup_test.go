// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"reflect"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/stretchr/testify/require"
)

func TestParseAst(t *testing.T) {
	parser := ast.NewAstParser()
	_, err := parser.ParseFileFast("setup_test.go")
	require.NoError(t, err)
}

func TestSplitCompileCmds(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		isWin    bool
		expected []string
	}{
		{
			name:     "basic split with quotes",
			input:    `"a b" c`,
			isWin:    false,
			expected: []string{"a b", "c"},
		},
		{
			name:     "quoted and unquoted mix",
			input:    `-o "my file.o" -p main`,
			isWin:    false,
			expected: []string{"-o", "my file.o", "-p", "main"},
		},
		{
			name:     "no quotes",
			input:    `-o file.o -p main`,
			isWin:    false,
			expected: []string{"-o", "file.o", "-p", "main"},
		},
		{
			name:     "Windows path unescaping",
			input:    "-o \"C:\\\\path\\\\to\\\\file.o\"",
			isWin:    true,
			expected: []string{"-o", `C:\path\to\file.o`},
		},
		{
			name:     "Trailing space",
			input:    `-o file.o `,
			isWin:    false,
			expected: []string{"-o", "file.o"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip Windows-only tests if not on Windows
			if tt.isWin && !util.IsWindows() {
				t.Skip("Skipping Windows-specific test on non-Windows system")
			}

			// Skip non-Windows-only tests if on Windows
			if !tt.isWin && util.IsWindows() {
				t.Skip("Skipping non-Windows-specific test on Windows system")
			}

			actual := splitCompileCmds(tt.input)
			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("Expected: %#v, got: %#v", tt.expected, actual)
			}
		})
	}
}
