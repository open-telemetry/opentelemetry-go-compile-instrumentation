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

func directiveIdentity(t *testing.T, name string, flat map[string]any) string {
	t.Helper()
	data, err := yaml.Marshal(flat)
	require.NoError(t, err)
	r, err := NewInstDirectiveRule(data, name)
	require.NoError(t, err)
	return r.Identity()
}

func TestInstDirectiveRule_Identity(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{"target": "main", "directive": "otelc:span", "template": "_ = 0"}
	}

	// De-duplication: identical content under different names is one identity.
	dupA := base()
	dupB := base()
	assert.Equal(t, directiveIdentity(t, "alpha", dupA), directiveIdentity(t, "beta", dupB),
		"identical rule content must share an identity regardless of name")

	// Differing target yields a distinct identity.
	diffTarget := base()
	diffTarget["target"] = "github.com/example/lib"
	assert.NotEqual(t, directiveIdentity(t, "r", base()), directiveIdentity(t, "r", diffTarget),
		"rules differing only in target must have distinct identities")

	// Differing version yields a distinct identity.
	diffVersion := base()
	diffVersion["version"] = "v1.0.0,v2.0.0"
	assert.NotEqual(t, directiveIdentity(t, "r", base()), directiveIdentity(t, "r", diffVersion),
		"rules differing only in version must have distinct identities")

	// Differing directive yields a distinct identity.
	diffDirective := base()
	diffDirective["directive"] = "otelc:trace"
	assert.NotEqual(t, directiveIdentity(t, "r", base()), directiveIdentity(t, "r", diffDirective),
		"rules differing only in directive must have distinct identities")

	// Differing template yields a distinct identity.
	diffTemplate := base()
	diffTemplate["template"] = "_ = 1"
	assert.NotEqual(t, directiveIdentity(t, "r", base()), directiveIdentity(t, "r", diffTemplate),
		"rules differing only in template must have distinct identities")
}
