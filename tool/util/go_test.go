// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsCompileCommand(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{
			name:     "valid compile command on Unix",
			line:     "/usr/local/go/pkg/tool/linux_amd64/compile -o /tmp/output.a -p main -buildid abc123",
			expected: !IsWindows(),
		},
		{
			name:     "valid compile command on Windows",
			line:     "C:\\Go\\pkg\\tool\\windows_amd64\\compile.exe -o C:\\tmp\\output.a -p main -buildid abc123",
			expected: true, // Contains compile.exe on any platform
		},
		{
			name:     "missing -o flag",
			line:     "/usr/local/go/pkg/tool/linux_amd64/compile -p main -buildid abc123",
			expected: false,
		},
		{
			name:     "missing -p flag",
			line:     "/usr/local/go/pkg/tool/linux_amd64/compile -o /tmp/output.a -buildid abc123",
			expected: false,
		},
		{
			name:     "missing -buildid flag",
			line:     "/usr/local/go/pkg/tool/linux_amd64/compile -o /tmp/output.a -p main",
			expected: false,
		},
		{
			name:     "missing compile executable",
			line:     "/usr/local/go/pkg/tool/linux_amd64/link -o /tmp/output.a -p main -buildid abc123",
			expected: false,
		},
		{
			name:     "PGO compile command should be excluded",
			line:     "/usr/local/go/pkg/tool/linux_amd64/compile -o /tmp/output.a -p main -buildid abc123 -pgoprofile /tmp/default.pgo",
			expected: false,
		},
		{
			name:     "complete compile command with additional flags",
			line:     "/usr/local/go/pkg/tool/linux_amd64/compile -o /tmp/output.a -trimpath -p main -buildid abc123 -goversion go1.21",
			expected: !IsWindows(),
		},
		{
			name:     "complete compile command with quoted paths",
			line:     `/usr/local/go/pkg/tool/linux_amd64/compile -o "/tmp/my output.a" -p main -buildid abc123`,
			expected: !IsWindows(),
		},
		{
			name:     "empty line",
			line:     "",
			expected: false,
		},
		{
			name:     "link command with compile in path",
			line:     "/home/user/go/pkg/tool/linux_amd64/link -o /tmp/output.a -p main -buildid abc123",
			expected: false,
		},
		{
			name:     "partial match should fail",
			line:     "compile -o output",
			expected: false,
		},
		{
			name:     "all required flags with importcfg",
			line:     "/usr/local/go/pkg/tool/linux_amd64/compile -o /tmp/output.a -p main -buildid abc123 -importcfg /tmp/importcfg",
			expected: !IsWindows(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsCompileCommand(tt.line)
			assert.Equal(t, tt.expected, result, "IsCompileCommand(%q) = %v, want %v", tt.line, result, tt.expected)
		})
	}
}

func TestIsCgoCommand(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{
			name:     "valid cgo command",
			line:     "/usr/local/go/pkg/tool/linux_amd64/cgo -objdir /tmp/cgo -importpath github.com/example/pkg",
			expected: true,
		},
		{
			name:     "valid cgo command with additional flags",
			line:     "cgo -objdir /tmp/cgo -importpath github.com/example/pkg -srcdir /home/user/project",
			expected: true,
		},
		{
			name:     "cgo command with quoted paths",
			line:     `cgo -objdir "/tmp/my cgo" -importpath github.com/example/pkg`,
			expected: true,
		},
		{
			name:     "missing cgo executable",
			line:     "/usr/local/go/pkg/tool/linux_amd64/link -objdir /tmp/obj -importpath github.com/example/pkg",
			expected: false,
		},
		{
			name:     "missing -objdir flag",
			line:     "cgo -importpath github.com/example/pkg",
			expected: false,
		},
		{
			name:     "missing -importpath flag",
			line:     "cgo -objdir /tmp/cgo",
			expected: false,
		},
		{
			name:     "cgo command with -dynimport should be excluded",
			line:     "cgo -objdir /tmp/cgo -importpath github.com/example/pkg -dynimport",
			expected: false,
		},
		{
			name:     "cgo command with -dynimport flag with value",
			line:     "cgo -objdir /tmp/cgo -importpath github.com/example/pkg -dynimport /tmp/output",
			expected: false,
		},
		{
			name:     "empty line",
			line:     "",
			expected: false,
		},
		{
			name:     "cgo in path but missing flags",
			line:     "/home/cgo/project/build",
			expected: false,
		},
		{
			name:     "partial match with only cgo and objdir",
			line:     "cgo -objdir /tmp/cgo",
			expected: false,
		},
		{
			name:     "partial match with only cgo and importpath",
			line:     "cgo -importpath github.com/example/pkg",
			expected: false,
		},
		{
			name:     "complete cgo command on Windows",
			line:     "C:\\Go\\pkg\\tool\\windows_amd64\\cgo.exe -objdir C:\\tmp\\cgo -importpath github.com/example/pkg",
			expected: true,
		},
		{
			name:     "cgo with all common flags",
			line:     "cgo -objdir /tmp/cgo -importpath github.com/example/pkg -exportheader /tmp/export.h -gccgo -gccgoprefix prefix",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsCgoCommand(tt.line)
			assert.Equal(t, tt.expected, result, "IsCgoCommand(%q) = %v, want %v", tt.line, result, tt.expected)
		})
	}
}

func TestFindFlagValue(t *testing.T) {
	tests := []struct {
		name     string
		cmd      []string
		flag     string
		expected string
	}{
		{
			name:     "flag found with value",
			cmd:      []string{"compile", "-o", "output.a", "-p", "main"},
			flag:     "-o",
			expected: "output.a",
		},
		{
			name:     "flag not found",
			cmd:      []string{"compile", "-o", "output.a", "-p", "main"},
			flag:     "-buildid",
			expected: "",
		},
		{
			name:     "empty command slice",
			cmd:      []string{},
			flag:     "-o",
			expected: "",
		},
		{
			name:     "flag with path containing spaces",
			cmd:      []string{"compile", "-o", "/path/to/my output.a", "-p", "main"},
			flag:     "-o",
			expected: "/path/to/my output.a",
		},
		{
			name:     "multiple occurrences returns first",
			cmd:      []string{"compile", "-flag", "first", "-flag", "second"},
			flag:     "-flag",
			expected: "first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FindFlagValue(tt.cmd, tt.flag)
			assert.Equal(t, tt.expected, result,
				"FindFlagValue(%v, %q) = %q, want %q", tt.cmd, tt.flag, result, tt.expected)
		})
	}
}

func TestIsGoFile(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "simple go file",
			path:     "main.go",
			expected: true,
		},
		{
			name:     "go file with path",
			path:     "/usr/local/src/project/main.go",
			expected: true,
		},
		{
			name:     "uppercase GO extension",
			path:     "main.GO",
			expected: true,
		},
		{
			name:     "non-go file",
			path:     "main.c",
			expected: false,
		},
		{
			name:     "go in filename but not extension",
			path:     "golang.c",
			expected: false,
		},
		{
			name:     "empty string",
			path:     "",
			expected: false,
		},
		{
			name:     "test go file",
			path:     "main_test.go",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsGoFile(tt.path)
			assert.Equal(t, tt.expected, result, "IsGoFile(%q) = %v, want %v", tt.path, result, tt.expected)
		})
	}
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
			if tt.isWin && !IsWindows() {
				t.Skip("Skipping Windows-specific test on non-Windows system")
			}

			// Skip non-Windows-only tests if on Windows
			if !tt.isWin && IsWindows() {
				t.Skip("Skipping non-Windows-specific test on Windows system")
			}

			actual := SplitCompileCmds(tt.input)
			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("Expected: %#v, got: %#v", tt.expected, actual)
			}
		})
	}
}
