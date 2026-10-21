// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/valyala/fasttemplate"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.yaml.in/yaml/v3"
)

// InstCallRule represents a rule that wraps function or method calls at call
// sites. Exactly one of FunctionCall or MethodCall must be set.
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
	MethodCall   string   `json:"method_call"   yaml:"method_call"`
	ImportPath   string   `json:"import-path"   yaml:"-"` // package import path, parsed from FunctionCall or MethodCall
	FuncName     string   `json:"func-name"     yaml:"-"` // function or method name, parsed from FunctionCall or MethodCall
	RecvType     string   `json:"recv-type"     yaml:"-"` // "Logger" or "*DB", parsed from MethodCall
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

// identPattern matches a single Go identifier
var identPattern = regexp.MustCompile(`^[^\d\W]\w*$`)

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

	if err := r.parseSelector(); err != nil {
		return nil, err
	}

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

// parseSelector parses whichever of FunctionCall or MethodCall is set.
func (r *InstCallRule) parseSelector() error {
	switch {
	case r.FunctionCall != "" && r.MethodCall != "":
		return ex.Newf("function_call and method_call are mutually exclusive")
	case r.FunctionCall != "":
		matches := funcNamePattern.FindStringSubmatch(r.FunctionCall)
		if matches == nil {
			return ex.Newf("invalid function_call format: %q (expected 'package/path.FunctionName')", r.FunctionCall)
		}
		r.ImportPath, r.FuncName = matches[1], matches[2]
		return nil
	case r.MethodCall != "":
		importPath, recvType, funcName, err := parseMethodCall(r.MethodCall)
		if err != nil {
			return err
		}
		r.ImportPath, r.RecvType, r.FuncName = importPath, recvType, funcName
		return nil
	default:
		return ex.Newf("one of function_call or method_call must be set")
	}
}

// parseMethodCall splits a method_call selector into import path, receiver
// type, and method name, in that order.
//
//nolint:revive // confusing-results conflicts with nonamedreturns
func parseMethodCall(methodCall string) (string, string, string, error) {
	invalid := func() error {
		return ex.Newf("invalid method_call format: %q (expected 'package/path.Type.Method')", methodCall)
	}

	matches := funcNamePattern.FindStringSubmatch(methodCall)
	if matches == nil {
		return "", "", "", invalid()
	}
	recvQualified, funcName := matches[1], matches[2]

	// A plain split, not funcNamePattern again: "*" fails its identifier group.
	dot := strings.LastIndex(recvQualified, ".")
	if dot < 0 {
		return "", "", "", invalid()
	}
	importPath := recvQualified[:dot]
	typeSeg := recvQualified[dot+1:]

	bareType := strings.TrimPrefix(typeSeg, "*")
	if importPath == "" || !identPattern.MatchString(bareType) {
		return "", "", "", invalid()
	}

	return importPath, typeSeg, funcName, nil
}

func (r *InstCallRule) validate() error {
	// parseSelector already validated the FunctionCall and MethodCall format.
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
		if err := r.parseSelector(); err != nil {
			return err
		}
	}

	return nil
}
