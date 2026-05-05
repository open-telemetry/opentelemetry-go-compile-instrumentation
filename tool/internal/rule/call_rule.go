// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/valyala/fasttemplate"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"gopkg.in/yaml.v3"
)

// InstCallRule represents a rule that wraps function calls at call sites.
//
// The function_call field must use the qualified format: "package/path.FunctionName"
// This matches calls to functions from a specific import path.
//
// Examples:
//   - "net/http.Get" matches http.Get() where http is imported from "net/http"
//   - "github.com/redis/go-redis/v9.Get" matches redis.Get() from that package
//
// Example rule:
//
//	wrap_http_get:
//		target: "main"
//		function_call: "net/http.Get"
//		template: "tracedGet({{ . }})"
//
// This transforms: http.Get("url")
// Into: tracedGet(http.Get("url"))
type InstCallRule struct {
	InstBaseRule `yaml:",inline"`

	// FunctionCall is the qualified function name from YAML (e.g., "net/http.Get")
	// This field is parsed into ImportPath and FuncName during rule creation.
	FunctionCall string `json:"function_call" yaml:"function_call"`

	// ImportPath is the parsed package import path (e.g., "net/http")
	// This field is populated during rule creation from FunctionCall.
	ImportPath string `json:"import-path" yaml:"-"`

	// FuncName is the parsed function name (e.g., "Get")
	// This field is populated during rule creation from FunctionCall.
	FuncName string `json:"func-name" yaml:"-"`

	// Template is the wrapper code with {{ . }} as placeholder for the original call.
	// The template must be a valid Go expression. The output may be any expression
	// type — it is not required to be a call expression.
	//
	// Examples:
	//   - "wrapper({{ . }})" wraps the call with wrapper()
	//   - "(func() { return {{ . }} })()" uses an IIFE
	//   - "otelhttp.NewTransport({{ . }})" replaces a transport value
	Template string `json:"template" yaml:"template"`

	// AppendArgs is a list of Go expression strings appended as additional
	// arguments to the matched call. See docs/rules.md for full semantics.
	AppendArgs []string `json:"append_args" yaml:"append_args"`

	// VariadicType is the element type of the variadic parameter (e.g. "grpc.DialOption").
	// Required only when the matched call uses an ellipsis spread (f(a, opts...)).
	// When set and the call is ellipsis, an IIFE wrapper is generated.
	// When unset and the call is ellipsis, the call is skipped with a warning.
	VariadicType string `json:"variadic_type" yaml:"variadic_type"`
}

// funcNamePattern matches qualified function names like "net/http.Get".
// The import path and function name must be separated by a dot.
//
// Pattern: ^(.+)\.([^\d\W]\w*)$
//   - Group 1 (required): Everything before the last dot = import path
//   - Group 2 (required): Everything after the last dot = function name
//
// Valid matches:
//   - "net/http.Get" → importPath="net/http", funcName="Get"
//   - "github.com/user/pkg.Method" → importPath="github.com/user/pkg", funcName="Method"
//   - "database/sql.Open" → importPath="database/sql", funcName="Open"
//
// Invalid (will not match):
//   - "Func1" (no package path)
//   - "123Invalid" (starts with digit)
//   - "" (empty string)
var funcNamePattern = regexp.MustCompile(`^(.+)\.([^\d\W]\w*)$`)

// templatePlaceholderPattern matches template placeholder variants:
// {{ . }}, {{.}}, {{- . -}}, {{ .  }}, etc.
var templatePlaceholderPattern = regexp.MustCompile(`\{\{-?\s*\.\s*-?\}\}`)

// NewInstCallRule loads and validates an InstCallRule from YAML data.
func NewInstCallRule(data []byte, name string) (*InstCallRule, error) {
	var r InstCallRule
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, ex.Wrap(err)
	}
	if r.Name == "" {
		r.Name = name
	}

	// Parse the qualified function name once at creation
	matches := funcNamePattern.FindStringSubmatch(r.FunctionCall)
	if matches == nil {
		return nil, ex.Newf("invalid function_call format: %q (expected 'package/path.FunctionName')", r.FunctionCall)
	}

	// Store parsed components
	r.ImportPath = matches[1]
	r.FuncName = matches[2]

	// Validate other fields
	if err := r.validate(); err != nil {
		return nil, ex.Wrapf(err, "invalid call rule %q", name)
	}

	// Validate template syntax
	if r.Template != "" {
		if _, err := fasttemplate.NewTemplate(r.Template, "{{", "}}"); err != nil {
			return nil, ex.Wrapf(err, "invalid template syntax for rule %q", name)
		}
	}

	return &r, nil
}

func (r *InstCallRule) validate() error {
	// FunctionCall format already validated in NewInstCallRule
	if strings.TrimSpace(r.FunctionCall) == "" {
		return ex.Newf("function_call cannot be empty")
	}

	if strings.TrimSpace(r.Template) == "" && len(r.AppendArgs) == 0 {
		return ex.Newf("at least one of template or append_args must be set")
	}
	if strings.TrimSpace(r.Template) != "" && !templatePlaceholderPattern.MatchString(r.Template) {
		return ex.Newf("template must contain {{ . }} placeholder (also accepts {{.}}, {{- . -}}, etc.)")
	}
	for i, arg := range r.AppendArgs {
		if strings.TrimSpace(arg) == "" {
			return ex.Newf("append_args[%d] must be a non-empty string", i)
		}
	}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler to ensure derived fields are populated
// after JSON deserialization.
func (r *InstCallRule) UnmarshalJSON(data []byte) error {
	// Use a type alias to avoid infinite recursion
	type Alias InstCallRule
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(r),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	// Parse ImportPath and FuncName if not already set
	if r.ImportPath == "" || r.FuncName == "" {
		matches := funcNamePattern.FindStringSubmatch(r.FunctionCall)
		if matches == nil {
			return ex.Newf("invalid function_call format: %q", r.FunctionCall)
		}
		r.ImportPath = matches[1]
		r.FuncName = matches[2]
	}

	return nil
}
