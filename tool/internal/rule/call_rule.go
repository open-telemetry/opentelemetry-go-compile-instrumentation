// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/valyala/fasttemplate"

	"go.opentelemetry.io/otelc/tool/ex"
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
//		replace: "tracedGet({{ . }})"
//
// This transforms: http.Get("url")
// Into: tracedGet(http.Get("url"))
type InstCallRule struct {
	InstBaseRule `yaml:",inline"`

	FunctionCall string   `json:"function_call" yaml:"function_call"`
	ImportPath   string   `json:"import-path"   yaml:"-"` // "net/http", parsed from FunctionCall
	FuncName     string   `json:"func-name"     yaml:"-"` // "Get", parsed from FunctionCall
	Replace      string   `json:"replace"       yaml:"replace"`
	AppendArgs   []string `json:"append_args"   yaml:"append_args"`
	VariadicType string   `json:"variadic_type" yaml:"variadic_type"`
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

// replacePlaceholderPattern matches replacement template placeholder variants:
// {{ . }}, {{.}}, {{- . -}}, {{ .  }}, etc.
var replacePlaceholderPattern = regexp.MustCompile(`\{\{-?\s*\.\s*-?\}\}`)

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

	// Validate replacement template syntax
	if r.Replace != "" {
		if _, err := fasttemplate.NewTemplate(r.Replace, "{{", "}}"); err != nil {
			return nil, ex.Wrapf(err, "invalid replace syntax for rule %q", name)
		}
	}

	return &r, nil
}

func (r *InstCallRule) validate() error {
	// FunctionCall format already validated in NewInstCallRule
	if strings.TrimSpace(r.FunctionCall) == "" {
		return ex.Newf("function_call cannot be empty")
	}

	if strings.TrimSpace(r.Replace) == "" && len(r.AppendArgs) == 0 {
		return ex.Newf("at least one of replace or append_args must be set")
	}
	if strings.TrimSpace(r.Replace) != "" && !replacePlaceholderPattern.MatchString(r.Replace) {
		return ex.Newf("replace must contain {{ . }} placeholder (also accepts {{.}}, {{- . -}}, etc.)")
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
