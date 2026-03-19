// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNewInstDirectiveRule(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		ruleName    string
		expectError bool
	}{
		{
			name: "valid directive",
			yamlContent: `
directive: "otelc:span"
target: main
`,
			ruleName:    "test-directive",
			expectError: false,
		},
		{
			name: "with version",
			yamlContent: `
directive: "dd:span"
target: github.com/example/lib
version: "v1.0.0,v2.0.0"
`,
			ruleName:    "versioned-directive",
			expectError: false,
		},
		{
			name: "empty directive",
			yamlContent: `
directive: ""
target: main
`,
			ruleName:    "empty-directive",
			expectError: true,
		},
		{
			name: "spaces in directive",
			yamlContent: `
directive: "dd span"
target: main
`,
			ruleName:    "spaces-directive",
			expectError: true,
		},
		{
			name: "slash prefix in directive",
			yamlContent: `
directive: "//dd:span"
target: main
`,
			ruleName:    "prefix-directive",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fields map[string]any
			err := yaml.Unmarshal([]byte(tt.yamlContent), &fields)
			require.NoError(t, err)

			data, err := yaml.Marshal(fields)
			require.NoError(t, err)

			r, err := NewInstDirectiveRule(data, tt.ruleName)
			if tt.expectError {
				require.Error(t, err)
				require.Nil(t, r)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, r)
			assert.Equal(t, tt.ruleName, r.GetName())
		})
	}
}
