// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
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
			dec:       "//dd:span",
			directive: "dd:span",
			expected:  true,
		},
		{
			name:      "leading whitespace",
			dec:       "\t//dd:span",
			directive: "dd:span",
			expected:  true,
		},
		{
			name:      "with args",
			dec:       "//dd:span key:val",
			directive: "dd:span",
			expected:  true,
		},
		{
			name:      "space after slashes",
			dec:       "// dd:span",
			directive: "dd:span",
			expected:  false,
		},
		{
			name:      "prefix match rejected",
			dec:       "//dd:span2",
			directive: "dd:span",
			expected:  false,
		},
		{
			name:      "block comment",
			dec:       "/*dd:span*/",
			directive: "dd:span",
			expected:  false,
		},
		{
			name:      "empty decoration",
			dec:       "",
			directive: "dd:span",
			expected:  false,
		},
		{
			name:      "just slashes",
			dec:       "//",
			directive: "dd:span",
			expected:  false,
		},
		{
			name:      "different directive",
			dec:       "//otelc:span",
			directive: "dd:span",
			expected:  false,
		},
		{
			name:      "tab after directive",
			dec:       "//dd:span\tkey:val",
			directive: "dd:span",
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
			dec:       `//dd:span span.name:"my op" tag:foo`,
			directive: "dd:span",
			expected: []DirectiveArg{
				{Key: "span.name", Value: "my op"},
				{Key: "tag", Value: "foo"},
			},
		},
		{
			name:      "directive without args",
			dec:       "//dd:span",
			directive: "dd:span",
			expected:  nil,
		},
		{
			name:      "non-matching decoration",
			dec:       "// regular comment",
			directive: "dd:span",
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
