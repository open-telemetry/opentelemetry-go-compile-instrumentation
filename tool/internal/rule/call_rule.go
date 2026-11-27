// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"bytes"
	"encoding/json"
	"maps"
	"regexp"
	"slices"
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

// callArgPlaceholderPattern matches call argument placeholder variants:
// {{ .CallArgument 0 }}, {{.CallArgument 0}}, {{- .CallArgument 1 -}},
// {{ $.CallArgument $i }}, etc.
// It only matches when .CallArgument directly follows {{, so valid forms such
// as {{ printf "%s" (.CallArgument 0) }} or {{ if .CallArgument 0 }} are
// rejected. This is an early check on the template text. The instrument
// package checks the rendered output for each call site.
var callArgPlaceholderPattern = regexp.MustCompile(`\{\{-?\s*\$?\.CallArgument\s+[^\s}][^}]*\}\}`)

// callPartsPlaceholderPattern matches the placeholders that render the
// receiver or all arguments of the matched call: {{ .CallReceiver }},
// {{.CallArguments}}, {{- $.CallReceiver -}}, {{ .CallArguments | printf }},
// etc.
var callPartsPlaceholderPattern = regexp.MustCompile(`\{\{-?\s*\$?\.Call(Receiver|Arguments)\s*(\|[^}]*)?-?\}\}`)

// callReceiverPattern matches a use of .CallReceiver anywhere in a template.
var callReceiverPattern = regexp.MustCompile(`\.CallReceiver\b`)

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
	if strings.TrimSpace(r.Replace) != "" &&
		!replacePlaceholderPattern.MatchString(r.Replace) &&
		!callArgPlaceholderPattern.MatchString(r.Replace) &&
		!callPartsPlaceholderPattern.MatchString(r.Replace) {
		return ex.Newf("replace must contain a {{ . }}, {{ .CallArgument N }}, {{ .CallArguments }} " +
			"or {{ .CallReceiver }} placeholder (also accepts {{.}}, {{- . -}}, {{.CallArgument 0}}, etc.)")
	}
	if r.FunctionCall != "" && callReceiverPattern.MatchString(r.Replace) {
		return ex.Newf("replace uses {{ .CallReceiver }}, which requires method_call, but the rule uses function_call")
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

// CallRulesByFile maps a source file to its call rules. Setup adds one call
// rule to many files: a method_call rule goes to every file of a package. The
// JSON form of the map holds each rule only once, and the files refer to the
// rules by their index.
type CallRulesByFile map[string][]*InstCallRule

// MarshalJSON implements json.Marshaler. Rules with the same JSON encoding are
// stored once.
func (m CallRulesByFile) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	var out struct {
		Rules []json.RawMessage `json:"rules"`
		Files map[string][]int  `json:"files"`
	}
	out.Files = make(map[string][]int, len(m))
	byRule := make(map[*InstCallRule]int)
	byEncoding := make(map[string]int)
	for _, file := range slices.Sorted(maps.Keys(m)) {
		indexes := make([]int, 0, len(m[file]))
		for _, r := range m[file] {
			index, ok := byRule[r]
			if !ok {
				data, err := json.Marshal(r)
				if err != nil {
					return nil, ex.Wrapf(err, "encoding call rule %q", r.GetName())
				}
				index, ok = byEncoding[string(data)]
				if !ok {
					index = len(out.Rules)
					out.Rules = append(out.Rules, data)
					byEncoding[string(data)] = index
				}
				byRule[r] = index
			}
			indexes = append(indexes, index)
		}
		out.Files[file] = indexes
	}
	return json.Marshal(out)
}

// UnmarshalJSON implements json.Unmarshaler. The files that refer to one rule
// share one *InstCallRule.
func (m *CallRulesByFile) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*m = nil
		return nil
	}
	var in struct {
		Rules []*InstCallRule  `json:"rules"`
		Files map[string][]int `json:"files"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return ex.Wrapf(err, "decoding call rules")
	}
	rules := make(CallRulesByFile, len(in.Files))
	for file, indexes := range in.Files {
		list := make([]*InstCallRule, 0, len(indexes))
		for _, index := range indexes {
			if index < 0 || index >= len(in.Rules) || in.Rules[index] == nil {
				return ex.Newf("call rules of %s refer to rule %d of %d", file, index, len(in.Rules))
			}
			list = append(list, in.Rules[index])
		}
		rules[file] = list
	}
	*m = rules
	return nil
}
