// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInstDeclRule(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		ruleName    string
		wantErr     bool
		errContains string
		check       func(*testing.T, *InstDeclRule)
	}{
		{
			name: "var rule with value",
			yaml: `
target: example.com/pkg
kind: var
identifier: GlobalVar
value: '"replaced"'
`,
			ruleName: "assign_global_var",
			check: func(t *testing.T, r *InstDeclRule) {
				assert.Equal(t, "assign_global_var", r.Name)
				assert.Equal(t, "example.com/pkg", r.Target)
				assert.Equal(t, "var", r.Kind)
				assert.Equal(t, "GlobalVar", r.Identifier)
				assert.Equal(t, `"replaced"`, r.Value)
			},
		},
		{
			name: "const rule with value",
			yaml: `
target: example.com/pkg
kind: const
identifier: MaxRetries
value: "42"
`,
			ruleName: "patch_const",
			check: func(t *testing.T, r *InstDeclRule) {
				assert.Equal(t, "const", r.Kind)
				assert.Equal(t, "MaxRetries", r.Identifier)
				assert.Equal(t, "42", r.Value)
			},
		},
		{
			name: "name from YAML overrides ruleName argument",
			yaml: `
name: yaml_name
target: example.com/pkg
identifier: SomeDecl
value: "42"
`,
			ruleName: "arg_name",
			check: func(t *testing.T, r *InstDeclRule) {
				assert.Equal(t, "yaml_name", r.Name)
			},
		},
		{
			name: "name from argument used when YAML name absent",
			yaml: `
target: example.com/pkg
identifier: SomeDecl
value: "42"
`,
			ruleName: "arg_name",
			check: func(t *testing.T, r *InstDeclRule) {
				assert.Equal(t, "arg_name", r.Name)
			},
		},
		{
			name: "neither value nor wrap",
			yaml: `
target: example.com/pkg
identifier: SomeDecl
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "one of value or wrap must be set",
		},
		{
			name: "whitespace-only value and no wrap",
			yaml: `
target: example.com/pkg
identifier: SomeDecl
value: "   "
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "one of value or wrap must be set",
		},
		{
			name: "both value and wrap set",
			yaml: `
target: example.com/pkg
identifier: SomeDecl
value: "42"
wrap: "wrapper({{ . }})"
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "value and wrap are mutually exclusive",
		},
		{
			name: "func kind without value or wrap",
			yaml: `
target: example.com/pkg
kind: func
identifier: MyFunc
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "has no supported advice",
		},
		{
			name: "type kind without value or wrap",
			yaml: `
target: example.com/pkg
kind: type
identifier: MyType
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "has no supported advice",
		},
		{
			name: "func kind with wrap",
			yaml: `
target: example.com/pkg
kind: func
identifier: MyFunc
wrap: "wrapper({{ . }})"
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "wrap is not valid when kind is",
		},
		{
			name: "wrap template missing placeholder",
			yaml: `
target: example.com/pkg
identifier: SomeDecl
wrap: "wrapper(x)"
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "wrap template must contain {{ . }} placeholder",
		},
		{
			name: "wrap valid",
			yaml: `
target: example.com/pkg
kind: var
identifier: DefaultTransport
wrap: "otelhttp.NewTransport({{ . }})"
imports:
  otelhttp: "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
`,
			ruleName: "wrap_default_transport",
			check: func(t *testing.T, r *InstDeclRule) {
				assert.Equal(t, "wrap_default_transport", r.Name)
				assert.Equal(t, "var", r.Kind)
				assert.Equal(t, "DefaultTransport", r.Identifier)
				assert.Empty(t, r.Value)
				assert.Equal(t, "otelhttp.NewTransport({{ . }})", r.Wrap)
			},
		},
		{
			name: "wrap compact placeholder variant",
			yaml: `
target: example.com/pkg
identifier: SomeDecl
wrap: "wrapper({{.}})"
`,
			ruleName: "wrap_some_decl",
			check: func(t *testing.T, r *InstDeclRule) {
				assert.Equal(t, "wrapper({{.}})", r.Wrap)
			},
		},
		{
			name: "empty identifier",
			yaml: `
target: example.com/pkg
identifier: ""
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "identifier cannot be empty",
		},
		{
			name: "whitespace-only identifier",
			yaml: `
target: example.com/pkg
identifier: "   "
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "identifier cannot be empty",
		},
		{
			name: "invalid kind",
			yaml: `
target: example.com/pkg
kind: interface
identifier: MyDecl
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "kind",
		},
		{
			name: "value not allowed with kind func",
			yaml: `
target: example.com/pkg
kind: func
identifier: MyFunc
value: "someExpr()"
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "value is not valid when kind is",
		},
		{
			name: "value not allowed with kind type",
			yaml: `
target: example.com/pkg
kind: type
identifier: MyType
value: "int"
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "value is not valid when kind is",
		},
		{
			name:     "invalid yaml",
			yaml:     `{bad yaml [`,
			ruleName: "bad_rule",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewInstDeclRule([]byte(tt.yaml), tt.ruleName)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, r)
			if tt.check != nil {
				tt.check(t, r)
			}
		})
	}
}
