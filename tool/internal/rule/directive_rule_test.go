// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
template: "_ = 0"
`,
			ruleName:    "test-directive",
			expectError: false,
		},
		{
			name: "with version",
			yamlContent: `
directive: "otelc:span"
target: github.com/example/lib
version: "v1.0.0,v2.0.0"
template: "_ = 0"
`,
			ruleName:    "versioned-directive",
			expectError: false,
		},
		{
			name: "missing template",
			yamlContent: `
directive: "otelc:span"
target: main
`,
			ruleName:    "no-template",
			expectError: true,
		},
		{
			name: "invalid template syntax",
			yamlContent: `
directive: "otelc:span"
target: main
template: "{{.Unclosed"
`,
			ruleName:    "bad-template",
			expectError: true,
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
directive: "//otelc:span"
target: main
`,
			ruleName:    "prefix-directive",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := newDirectiveRuleFromFlat(t, tt.ruleName, tt.yamlContent)
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

// newDirectiveRuleFromFlat builds an InstDirectiveRule from a flat fixture:
// directive is the where selector; template the expand_directive payload.
func newDirectiveRuleFromFlat(t *testing.T, name, yamlStr string) (*InstDirectiveRule, error) {
	t.Helper()
	flat, err := flatMap(yamlStr)
	if err != nil {
		return nil, err
	}
	base, rest := splitBase(flat, name)
	act := &DirectiveAction{}
	if v, ok := rest["template"].(string); ok {
		act.Template = v
		delete(rest, "template")
	}
	return NewInstDirectiveRule(base, mapNode(t, rest), act)
}
