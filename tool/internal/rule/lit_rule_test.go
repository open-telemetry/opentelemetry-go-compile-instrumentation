// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInstLitRule(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		ruleName    string
		wantErr     bool
		errContains string
		check       func(*testing.T, *InstLitRule)
	}{
		{
			name: "single field",
			yaml: `
struct_literal: net/http.Transport
field:
  - name: Internal
    value: "true"
`,
			ruleName: "mark_internal",
			check: func(t *testing.T, r *InstLitRule) {
				assert.Equal(t, "mark_internal", r.Name)
				assert.Equal(t, "net/http.Transport", r.StructLiteral)
				assert.Equal(t, "net/http", r.ImportPath)
				assert.Equal(t, "Transport", r.TypeName)
				require.Len(t, r.Field, 1)
				assert.Equal(t, "Internal", r.Field[0].Name)
				assert.Equal(t, "true", r.Field[0].Value)
			},
		},
		{
			name: "multiple fields",
			yaml: `
struct_literal: example.com/pkg.Config
field:
  - name: A
    value: "1"
  - name: B
    value: '"two"'
`,
			ruleName: "set_config",
			check: func(t *testing.T, r *InstLitRule) {
				assert.Equal(t, "example.com/pkg", r.ImportPath)
				assert.Equal(t, "Config", r.TypeName)
				require.Len(t, r.Field, 2)
				assert.Equal(t, "B", r.Field[1].Name)
			},
		},
		{
			name: "name from YAML overrides argument",
			yaml: `
name: yaml_name
struct_literal: net/http.Transport
field:
  - name: Internal
    value: "true"
`,
			ruleName: "arg_name",
			check: func(t *testing.T, r *InstLitRule) {
				assert.Equal(t, "yaml_name", r.Name)
			},
		},
		{
			name: "invalid struct_literal format",
			yaml: `
struct_literal: NoPackagePath
field:
  - name: Internal
    value: "true"
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "invalid struct_literal format",
		},
		{
			name: "no fields",
			yaml: `
struct_literal: net/http.Transport
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "field must list at least one field to set",
		},
		{
			name: "empty field name",
			yaml: `
struct_literal: net/http.Transport
field:
  - name: "  "
    value: "true"
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "field[0].name cannot be empty",
		},
		{
			name: "wrap only",
			yaml: `
struct_literal: net/http.Client
field:
  - name: Transport
    wrap: "otelhttp.NewTransport({{ . }})"
`,
			ruleName: "instrument_clients",
			check: func(t *testing.T, r *InstLitRule) {
				require.Len(t, r.Field, 1)
				assert.Empty(t, r.Field[0].Value)
				assert.Equal(t, "otelhttp.NewTransport({{ . }})", r.Field[0].Wrap)
			},
		},
		{
			name: "value and wrap together",
			yaml: `
struct_literal: net/http.Client
field:
  - name: Transport
    wrap: "otelhttp.NewTransport({{ . }})"
    value: "otelhttp.NewTransport(http.DefaultTransport)"
`,
			ruleName: "instrument_clients",
			check: func(t *testing.T, r *InstLitRule) {
				require.Len(t, r.Field, 1)
				assert.Equal(t, "otelhttp.NewTransport(http.DefaultTransport)", r.Field[0].Value)
				assert.Equal(t, "otelhttp.NewTransport({{ . }})", r.Field[0].Wrap)
			},
		},
		{
			name: "neither value nor wrap",
			yaml: `
struct_literal: net/http.Transport
field:
  - name: Internal
    value: ""
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "field[0] must set one of value or wrap",
		},
		{
			name: "field with only a name",
			yaml: `
struct_literal: net/http.Transport
field:
  - name: Internal
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "field[0] must set one of value or wrap",
		},
		{
			name: "wrap without placeholder",
			yaml: `
struct_literal: net/http.Client
field:
  - name: Transport
    wrap: "otelhttp.NewTransport()"
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "field[0].wrap must contain {{ . }} placeholder",
		},
		{
			name: "duplicate field name",
			yaml: `
struct_literal: net/http.Transport
field:
  - name: Internal
    value: "true"
  - name: Internal
    value: "false"
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: `field "Internal" is set more than once`,
		},
		{
			name:     "invalid yaml",
			yaml:     `{bad yaml [`,
			ruleName: "bad",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewInstLitRule([]byte(tt.yaml), tt.ruleName)
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

func TestInstLitRule_UnmarshalJSON(t *testing.T) {
	t.Run("populates derived fields", func(t *testing.T) {
		data := `{"struct_literal":"net/http.Transport","field":[{"name":"Internal","value":"true"}]}`
		var r InstLitRule
		err := json.Unmarshal([]byte(data), &r)
		require.NoError(t, err)
		assert.Equal(t, "net/http", r.ImportPath)
		assert.Equal(t, "Transport", r.TypeName)
		require.Len(t, r.Field, 1)
		assert.Equal(t, "Internal", r.Field[0].Name)
	})

	t.Run("skips re-parsing when derived fields already set", func(t *testing.T) {
		data := `{"struct_literal":"net/http.Transport","import-path":"already/set","type-name":"AlreadySet"}`
		var r InstLitRule
		err := json.Unmarshal([]byte(data), &r)
		require.NoError(t, err)
		assert.Equal(t, "already/set", r.ImportPath)
		assert.Equal(t, "AlreadySet", r.TypeName)
	})

	t.Run("invalid struct_literal format", func(t *testing.T) {
		data := `{"struct_literal":"NoPackage"}`
		var r InstLitRule
		err := json.Unmarshal([]byte(data), &r)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid struct_literal format")
	})

	t.Run("invalid json", func(t *testing.T) {
		var r InstLitRule
		err := json.Unmarshal([]byte(`{bad`), &r)
		require.Error(t, err)
	})
}
