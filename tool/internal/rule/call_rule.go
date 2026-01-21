// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/instrument/template"
	"gopkg.in/yaml.v3"
)

// InstCallRule represents a rule that wraps function calls at call sites.
//
// The function-call field must use the qualified format: "package/path.FunctionName"
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
//		function-call: "net/http.Get"
//		template: "instrumentation.tracedGet({{ . }})"
//		imports:
//			instrumentation: "myapp/instrumentation"
//
// This transforms: http.Get("url")
// Into: instrumentation.tracedGet(http.Get("url"))
type InstCallRule struct {
	InstBaseRule `yaml:",inline"`

	// FunctionCall is the qualified function name from YAML (e.g., "net/http.Get")
	// This field is parsed into ImportPath and FuncName during rule creation.
	FunctionCall string `json:"function-call" yaml:"function-call"`

	// ImportPath is the parsed package import path (e.g., "net/http")
	// This field is populated during rule creation from FunctionCall.
	ImportPath string `json:"import-path" yaml:"-"`

	// FuncName is the parsed function name (e.g., "Get")
	// This field is populated during rule creation from FunctionCall.
	FuncName string `json:"func-name" yaml:"-"`

	// Template is the wrapper code with {{ . }} as placeholder for the original call.
	// The template must be a valid Go expression.
	//
	// Examples:
	//   - "wrapper({{ . }})" wraps the call with wrapper()
	//   - "(func() { return {{ . }} })()" uses an IIFE
	Template string `json:"template" yaml:"template"`

	// Imports specifies additional imports needed by the template code.
	// Map key is the import alias, value is the import path.
	//
	// Example:
	//   imports:
	//     unsafe: "unsafe"
	//     tracer: "myapp/tracing"
	Imports map[string]string `json:"imports,omitempty" yaml:"imports,omitempty"`

	// CompiledTemplate is the compiled template object, created at rule creation time.
	// This field is not serialized.
	CompiledTemplate *template.Template `json:"-" yaml:"-"`
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
		return nil, ex.Newf("invalid function-call format: %q (expected 'package/path.FunctionName')", r.FunctionCall)
	}

	// Store parsed components
	r.ImportPath = matches[1]
	r.FuncName = matches[2]

	// Validate other fields
	if err := r.validate(); err != nil {
		return nil, ex.Wrapf(err, "invalid call rule %q", name)
	}

	// Compile the template once at creation time
	tmpl, err := template.NewTemplate(r.Template)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to compile template for rule %q", name)
	}
	r.CompiledTemplate = tmpl

	return &r, nil
}

func (r *InstCallRule) validate() error {
	// FunctionCall format already validated in NewInstCallRule
	if strings.TrimSpace(r.FunctionCall) == "" {
		return ex.Newf("function-call cannot be empty")
	}

	if strings.TrimSpace(r.Template) == "" {
		return ex.Newf("template cannot be empty")
	}
	if !strings.Contains(r.Template, "{{ . }}") {
		return ex.Newf("template must contain {{ . }} placeholder")
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
			return ex.Newf("invalid function-call format: %q", r.FunctionCall)
		}
		r.ImportPath = matches[1]
		r.FuncName = matches[2]
	}

	// Compile the template if not already compiled
	if r.CompiledTemplate == nil && r.Template != "" {
		tmpl, err := template.NewTemplate(r.Template)
		if err != nil {
			return ex.Wrapf(err, "failed to compile template")
		}
		r.CompiledTemplate = tmpl
	}

	return nil
}
