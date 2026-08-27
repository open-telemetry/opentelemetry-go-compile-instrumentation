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
			_, ok := matchDirective(tt.dec, tt.directive)
			assert.Equal(t, tt.expected, ok)
		})
	}
}

func TestScanArgs(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    []DirectiveArg
		errContains string // non-empty pins the error to the expected branch
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
			name:        "single quotes rejected",
			input:       "key:'single'",
			errContains: "single-quoted values are not supported",
		},
		{
			name:        "unclosed quote",
			input:       `key:"unclosed`,
			errContains: "unclosed double quote",
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
			name:        "missing colon",
			input:       "nocolon",
			errContains: "has no unquoted colon separator",
		},
		{
			name:  "empty value",
			input: "key:",
			expected: []DirectiveArg{
				{Key: "key", Value: ""},
			},
		},
		{
			name:        "empty key rejected",
			input:       ":value",
			errContains: "has an empty key",
		},
		{
			name:        "bare colon rejected",
			input:       ":",
			errContains: "has an empty key",
		},
		{
			name:        "fully quoted token has no unquoted colon separator",
			input:       `"key:value"`,
			errContains: "has no unquoted colon separator",
		},
		{
			name:        "quoted key rejected",
			input:       `"k":v`,
			errContains: "has a quoted or malformed key",
		},
		{
			name:        "escaped quote inside quoted key does not end the quote early",
			input:       `"a\"b":value`,
			errContains: "has a quoted or malformed key",
		},
		{
			name:  "quoted value containing colon",
			input: `url:"https://example.com/path"`,
			expected: []DirectiveArg{
				{Key: "url", Value: "https://example.com/path"},
			},
		},
		{
			name:  "bare value containing colon splits at first colon only",
			input: "key:a:b:c",
			expected: []DirectiveArg{
				{Key: "key", Value: "a:b:c"},
			},
		},
		{
			name:  "multiple args one with quoted colon value",
			input: `op:"http:post" tag:foo`,
			expected: []DirectiveArg{
				{Key: "op", Value: "http:post"},
				{Key: "tag", Value: "foo"},
			},
		},
		{
			// Backslash outside a quoted region is not an escape in either
			// tokenize or cutUnquoted. This is a characterization test: it
			// records today's behaviour so the change is visible if someone
			// later adds bare-backslash escaping.
			name:  "backslash in a bare key is not an escape",
			input: `k\:v`,
			expected: []DirectiveArg{
				{Key: `k\`, Value: "v"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scanArgs(tt.input)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
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
			result, err := parseDirectiveArgs(tt.dec, tt.directive)
			if tt.hasError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindFuncsByDirective(t *testing.T) {
	src := `package p
//otelc:span span.name:"custom-op" tag:foo
func Foo() {}

//otelc:span
func Bar() {}

func Baz() {}
`
	path := writeGoTempFile(t, src)
	tree, err := ParseFileFast(path)
	require.NoError(t, err)

	matches, err := FindFuncsByDirective(tree, "otelc:span")
	require.NoError(t, err)
	require.Len(t, matches, 2)

	assert.Equal(t, "Foo", matches[0].Func.Name.Name)
	assert.Equal(t, []DirectiveArg{{Key: "span.name", Value: "custom-op"}, {Key: "tag", Value: "foo"}}, matches[0].Args)

	assert.Equal(t, "Bar", matches[1].Func.Name.Name)
	assert.Empty(t, matches[1].Args)
}

func TestFindFuncsByDirective_NoMatches(t *testing.T) {
	src := `package p
func Foo() {}
`
	path := writeGoTempFile(t, src)
	tree, err := ParseFileFast(path)
	require.NoError(t, err)

	matches, err := FindFuncsByDirective(tree, "otelc:span")
	require.NoError(t, err)
	assert.Empty(t, matches)
}

func TestFindFuncsByDirective_SkipsNonFuncDecls(t *testing.T) {
	src := `package p
//otelc:span
type T struct{}

//otelc:span
func Foo() {}
`
	path := writeGoTempFile(t, src)
	tree, err := ParseFileFast(path)
	require.NoError(t, err)

	matches, err := FindFuncsByDirective(tree, "otelc:span")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "Foo", matches[0].Func.Name.Name)
}

func TestFindFuncsByDirective_ParseArgsError(t *testing.T) {
	src := `package p
//otelc:span nocolon
func Foo() {}
`
	path := writeGoTempFile(t, src)
	tree, err := ParseFileFast(path)
	require.NoError(t, err)

	matches, err := FindFuncsByDirective(tree, "otelc:span")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Foo")
	assert.Nil(t, matches)
}

func TestFindFuncsByDirective_FirstMatchingDecorationWins(t *testing.T) {
	src := `package p
// a regular doc comment
//otelc:span tag:first
//otelc:span tag:second
func Foo() {}
`
	path := writeGoTempFile(t, src)
	tree, err := ParseFileFast(path)
	require.NoError(t, err)

	matches, err := FindFuncsByDirective(tree, "otelc:span")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, []DirectiveArg{{Key: "tag", Value: "first"}}, matches[0].Args)
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

func TestFileHasLeadingDirective(t *testing.T) {
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
			expected:  false,
		},
		{
			name: "file level directive",
			src: `//otelc:span
package p
func Foo() {}
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
			assert.Equal(t, tt.expected, FileHasLeadingDirective(tree, tt.directive))
		})
	}
}
