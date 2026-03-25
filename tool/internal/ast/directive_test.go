// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchDirective(t *testing.T) {
	tests := []struct {
		name      string
		dec       string
		directive string
		expected  bool
	}{
		{
			name:      "exact match",
			dec:       "//otelc:span",
			directive: "otelc:span",
			expected:  true,
		},
		{
			name:      "leading whitespace",
			dec:       "\t//otelc:span",
			directive: "otelc:span",
			expected:  true,
		},
		{
			name:      "with args",
			dec:       "//otelc:span key:val",
			directive: "otelc:span",
			expected:  true,
		},
		{
			name:      "space after slashes",
			dec:       "// otelc:span",
			directive: "otelc:span",
			expected:  false,
		},
		{
			name:      "prefix match rejected",
			dec:       "//otelc:span2",
			directive: "otelc:span",
			expected:  false,
		},
		{
			name:      "block comment",
			dec:       "/*otelc:span*/",
			directive: "otelc:span",
			expected:  false,
		},
		{
			name:      "empty decoration",
			dec:       "",
			directive: "otelc:span",
			expected:  false,
		},
		{
			name:      "just slashes",
			dec:       "//",
			directive: "otelc:span",
			expected:  false,
		},
		{
			name:      "different directive",
			dec:       "//otelc:trace",
			directive: "otelc:span",
			expected:  false,
		},
		{
			name:      "tab after directive",
			dec:       "//otelc:span\tkey:val",
			directive: "otelc:span",
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchDirective(tt.dec, tt.directive)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestScanArgs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []DirectiveArg
		hasError bool
	}{
		{
			name:     "simple key:value",
			input:    "key:value",
			expected: []DirectiveArg{{Key: "key", Value: "value"}},
		},
		{
			name:  "quoted value with spaces",
			input: `span.name:"my operation" tag:simple`,
			expected: []DirectiveArg{
				{Key: "span.name", Value: "my operation"},
				{Key: "tag", Value: "simple"},
			},
		},
		{
			name:  "go escape in quoted value",
			input: `key:"hello\nworld"`,
			expected: []DirectiveArg{
				{Key: "key", Value: "hello\nworld"},
			},
		},
		{
			name:     "single quotes rejected",
			input:    "key:'single'",
			hasError: true,
		},
		{
			name:     "unclosed quote",
			input:    `key:"unclosed`,
			hasError: true,
		},
		{
			name:     "empty input",
			input:    "",
			expected: nil,
		},
		{
			name:  "extra whitespace",
			input: "  key1:v1   key2:v2  ",
			expected: []DirectiveArg{
				{Key: "key1", Value: "v1"},
				{Key: "key2", Value: "v2"},
			},
		},
		{
			name:     "missing colon",
			input:    "nocolon",
			hasError: true,
		},
		{
			name:  "empty value",
			input: "key:",
			expected: []DirectiveArg{
				{Key: "key", Value: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scanArgs(tt.input)
			if tt.hasError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseDirectiveArgs(t *testing.T) {
	tests := []struct {
		name      string
		dec       string
		directive string
		expected  []DirectiveArg
		hasError  bool
	}{
		{
			name:      "directive with args",
			dec:       `//otelc:span span.name:"my op" tag:foo`,
			directive: "otelc:span",
			expected: []DirectiveArg{
				{Key: "span.name", Value: "my op"},
				{Key: "tag", Value: "foo"},
			},
		},
		{
			name:      "directive without args",
			dec:       "//otelc:span",
			directive: "otelc:span",
			expected:  nil,
		},
		{
			name:      "non-matching decoration",
			dec:       "// regular comment",
			directive: "otelc:span",
			hasError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseDirectiveArgs(tt.dec, tt.directive)
			if tt.hasError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func writeGoTempFile(t *testing.T, src string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.go")
	require.NoError(t, err)
	_, err = f.WriteString(src)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func TestFileHasDirective(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		directive string
		expected  bool
	}{
		{
			name: "directive on function",
			src: `package p
//otelc:span
func Foo() {}
`,
			directive: "otelc:span",
			expected:  true,
		},
		{
			name: "directive with args",
			src: `package p
//otelc:span span.name:"op"
func Foo() {}
`,
			directive: "otelc:span",
			expected:  true,
		},
		{
			name: "no directive",
			src: `package p
// just a regular comment
func Foo() {}
`,
			directive: "otelc:span",
			expected:  false,
		},
		{
			name: "different directive",
			src: `package p
//otelc:trace
func Foo() {}
`,
			directive: "otelc:span",
			expected:  false,
		},
		{
			name: "prefix match rejected",
			src: `package p
//otelc:span2
func Foo() {}
`,
			directive: "otelc:span",
			expected:  false,
		},
		{
			name: "space after slashes rejected",
			src: `package p
// otelc:span
func Foo() {}
`,
			directive: "otelc:span",
			expected:  false,
		},
		{
			name: "directive on method",
			src: `package p
type T struct{}
//otelc:span
func (T) Bar() {}
`,
			directive: "otelc:span",
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeGoTempFile(t, tt.src)
			tree, err := ParseFileFast(path)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, FileHasDirective(tree, tt.directive))
		})
	}
}
