// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripBuildIgnore(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips go:build ignore",
			input:    "//go:build ignore\n\npackage main\n",
			expected: "\n\npackage main\n",
		},
		{
			name:     "unchanged without tag",
			input:    "package main\n",
			expected: "package main\n",
		},
		{
			// Regression test for #1069.
			name: "preserves tag text inside string literal and comment prose",
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
			assert.Equal(t, tt.expected, StripBuildIgnore(tt.input))
		})
	}
}
