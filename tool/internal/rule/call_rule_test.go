// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInstCallRule(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		ruleName    string
		wantErr     bool
		errContains string
		check       func(*testing.T, *InstCallRule)
	}{
		{
			name: "replace only",
			yaml: `
function_call: net/http.Get
replace: "wrapper({{ . }})"
`,
			ruleName: "wrap_http_get",
			check: func(t *testing.T, r *InstCallRule) {
				assert.Equal(t, "wrap_http_get", r.Name)
				assert.Equal(t, "net/http.Get", r.FunctionCall)
				assert.Equal(t, "net/http", r.ImportPath)
				assert.Equal(t, "Get", r.FuncName)
				assert.Equal(t, "wrapper({{ . }})", r.Replace)
			},
		},
		{
			name: "append_args only",
			yaml: `
function_call: net/http.Get
append_args: ["ctx"]
`,
			ruleName: "append_ctx",
			check: func(t *testing.T, r *InstCallRule) {
				assert.Equal(t, "net/http", r.ImportPath)
				assert.Equal(t, "Get", r.FuncName)
				assert.Equal(t, []string{"ctx"}, r.AppendArgs)
				assert.Empty(t, r.Replace)
			},
		},
		{
			name: "append_args with variadic_type",
			yaml: `
function_call: google.golang.org/grpc.Dial
append_args: ["myOpt"]
variadic_type: "grpc.DialOption"
`,
			ruleName: "inject_grpc_option",
			check: func(t *testing.T, r *InstCallRule) {
				assert.Equal(t, "grpc.DialOption", r.VariadicType)
				assert.Equal(t, []string{"myOpt"}, r.AppendArgs)
			},
		},
		{
			name: "both replace and append_args",
			yaml: `
function_call: net/http.Get
replace: "wrapper({{ . }})"
append_args: ["ctx"]
`,
			ruleName: "combined",
			check: func(t *testing.T, r *InstCallRule) {
				assert.NotEmpty(t, r.Replace)
				assert.NotEmpty(t, r.AppendArgs)
			},
		},
		{
			name: "name from YAML overrides argument",
			yaml: `
name: yaml_name
function_call: net/http.Get
replace: "wrapper({{ . }})"
`,
			ruleName: "arg_name",
			check: func(t *testing.T, r *InstCallRule) {
				assert.Equal(t, "yaml_name", r.Name)
			},
		},
		{
			name: "invalid function_call format",
			yaml: `
function_call: NoPackagePath
replace: "wrapper({{ . }})"
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "invalid function_call format",
		},
		{
			name: "neither replace nor append_args",
			yaml: `
function_call: net/http.Get
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "at least one of replace or append_args must be set",
		},
		{
			name: "replace without placeholder",
			yaml: `
function_call: net/http.Get
replace: "noPlaceholder()"
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "replace must contain {{ . }} placeholder",
		},
		{
			name: "empty append_args entry",
			yaml: `
function_call: net/http.Get
append_args: [""]
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "append_args[0] must be a non-empty string",
		},
		{
			name: "whitespace-only append_args entry",
			yaml: `
function_call: net/http.Get
append_args: ["   "]
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "append_args[0] must be a non-empty string",
		},
		{
			name: "invalid replace syntax",
			yaml: `
function_call: net/http.Get
replace: "wrapper({{ . }}) {{ unclosed"
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "invalid replace syntax",
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
			r, err := NewInstCallRule([]byte(tt.yaml), tt.ruleName)
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

func TestInstCallRule_UnmarshalJSON(t *testing.T) {
	t.Run("populates derived fields", func(t *testing.T) {
		data := `{"function_call":"net/http.Get","replace":"wrapper({{ . }})"}`
		var r InstCallRule
		err := json.Unmarshal([]byte(data), &r)
		require.NoError(t, err)
		assert.Equal(t, "net/http", r.ImportPath)
		assert.Equal(t, "Get", r.FuncName)
	})

	t.Run("skips re-parsing when derived fields already set", func(t *testing.T) {
		data := `{"function_call":"net/http.Get","import-path":"already/set","func-name":"AlreadySet"}`
		var r InstCallRule
		err := json.Unmarshal([]byte(data), &r)
		require.NoError(t, err)
		assert.Equal(t, "already/set", r.ImportPath)
		assert.Equal(t, "AlreadySet", r.FuncName)
	})

	t.Run("invalid function_call format", func(t *testing.T) {
		data := `{"function_call":"NoPackage"}`
		var r InstCallRule
		err := json.Unmarshal([]byte(data), &r)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid function_call format")
	})

	t.Run("invalid json", func(t *testing.T) {
		var r InstCallRule
		err := json.Unmarshal([]byte(`{bad`), &r)
		require.Error(t, err)
	})

	t.Run("append_args and variadic_type round-trip", func(t *testing.T) {
		data := `{"function_call":"net/http.Get","append_args":["ctx"],"variadic_type":"http.Option"}`
		var r InstCallRule
		err := json.Unmarshal([]byte(data), &r)
		require.NoError(t, err)
		assert.Equal(t, []string{"ctx"}, r.AppendArgs)
		assert.Equal(t, "http.Option", r.VariadicType)
	})
}
