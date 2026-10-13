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
		{
			name: "method_call value receiver",
			yaml: `
method_call: go.uber.org/zap.Logger.Info
replace: "tracedInfo({{ . }})"
`,
			ruleName: "wrap_zap_info",
			check: func(t *testing.T, r *InstCallRule) {
				assert.Equal(t, "go.uber.org/zap.Logger.Info", r.MethodCall)
				assert.Equal(t, "go.uber.org/zap", r.ImportPath)
				assert.Equal(t, "Logger", r.RecvType)
				assert.Equal(t, "Info", r.FuncName)
				assert.Empty(t, r.FunctionCall)
			},
		},
		{
			name: "method_call pointer receiver",
			yaml: `
method_call: database/sql.*DB.QueryContext
replace: "traced({{ . }})"
`,
			ruleName: "wrap_db_query",
			check: func(t *testing.T, r *InstCallRule) {
				assert.Equal(t, "database/sql", r.ImportPath)
				assert.Equal(t, "*DB", r.RecvType)
				assert.Equal(t, "QueryContext", r.FuncName)
			},
		},
		{
			name: "method_call and function_call are mutually exclusive",
			yaml: `
function_call: net/http.Get
method_call: go.uber.org/zap.Logger.Info
replace: "traced({{ . }})"
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "mutually exclusive",
		},
		{
			name: "neither function_call nor method_call set",
			yaml: `
replace: "traced({{ . }})"
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "one of function_call or method_call must be set",
		},
		{
			name: "invalid method_call format missing method",
			yaml: `
method_call: go.uber.org/zap.Logger
replace: "traced({{ . }})"
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "invalid method_call format",
		},
		{
			name: "invalid method_call format bad type name",
			yaml: `
method_call: go.uber.org/zap.123Logger.Info
replace: "traced({{ . }})"
`,
			ruleName:    "bad",
			wantErr:     true,
			errContains: "invalid method_call format",
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

	t.Run("method_call populates derived fields", func(t *testing.T) {
		data := `{"method_call":"go.uber.org/zap.Logger.Info","replace":"wrapper({{ . }})"}`
		var r InstCallRule
		err := json.Unmarshal([]byte(data), &r)
		require.NoError(t, err)
		assert.Equal(t, "go.uber.org/zap", r.ImportPath)
		assert.Equal(t, "Logger", r.RecvType)
		assert.Equal(t, "Info", r.FuncName)
	})
}

func TestParseMethodCall(t *testing.T) {
	tests := []struct {
		name           string
		methodCall     string
		wantImportPath string
		wantRecvType   string
		wantFuncName   string
		wantErr        bool
	}{
		{
			name:           "value receiver",
			methodCall:     "go.uber.org/zap.Logger.Info",
			wantImportPath: "go.uber.org/zap",
			wantRecvType:   "Logger",
			wantFuncName:   "Info",
		},
		{
			name:           "pointer receiver",
			methodCall:     "database/sql.*DB.QueryContext",
			wantImportPath: "database/sql",
			wantRecvType:   "*DB",
			wantFuncName:   "QueryContext",
		},
		{
			name:           "single-segment import path",
			methodCall:     "fmt.Stringer.String",
			wantImportPath: "fmt",
			wantRecvType:   "Stringer",
			wantFuncName:   "String",
		},
		{
			name:       "missing method name",
			methodCall: "go.uber.org/zap.Logger",
			wantErr:    true,
		},
		{
			name:       "missing receiver type",
			methodCall: "Logger.Info",
			wantErr:    true,
		},
		{
			name:       "no dots at all",
			methodCall: "Info",
			wantErr:    true,
		},
		{
			name:       "type name starts with a digit",
			methodCall: "go.uber.org/zap.123Logger.Info",
			wantErr:    true,
		},
		{
			name:       "empty string",
			methodCall: "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			importPath, recvType, funcName, err := parseMethodCall(tt.methodCall)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantImportPath, importPath)
			assert.Equal(t, tt.wantRecvType, recvType)
			assert.Equal(t, tt.wantFuncName, funcName)
		})
	}
}
