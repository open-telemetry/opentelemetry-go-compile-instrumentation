// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"encoding/json"
	"regexp"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"gopkg.in/yaml.v3"
)

// InstLitField represents a single field to set on a matched composite literal.
//
// At least one of Value or Wrap must be set. Unlike assign_value's mutually
// exclusive replace/wrap, both may be set together, because whether a field is
// present varies from one literal to the next.
type InstLitField struct {
	Name  string `json:"name"            yaml:"name"`
	Value string `json:"value,omitempty" yaml:"value,omitempty"`
	Wrap  string `json:"wrap,omitempty"  yaml:"wrap,omitempty"`
}

// InstLitRule represents a rule that sets fields on composite literals of a
// given struct type, wherever those literals are written.
//
// The struct_literal field uses the qualified format "package/path.TypeName",
// matching the convention of function_call. Unqualified literals of a type
// declared in the target package itself are not matched.
//
// Example rule:
//
//	mark_internal_transports:
//		target: "example.com/tracer"
//		struct_literal: "net/http.Transport"
//		field:
//		  - name: Internal
//		    value: "true"
//
// This transforms: &http.Transport{MaxIdleConns: 100}
// Into: &http.Transport{Internal: true, MaxIdleConns: 100}
type InstLitRule struct {
	InstBaseRule `yaml:",inline"`

	StructLiteral string          `json:"struct_literal" yaml:"struct_literal"`
	ImportPath    string          `json:"import-path"    yaml:"-"` // "net/http", parsed from StructLiteral
	TypeName      string          `json:"type-name"      yaml:"-"` // "Transport", parsed from StructLiteral
	Field         []*InstLitField `json:"field"          yaml:"field"`
}

// typeNamePattern matches qualified type names like "net/http.Transport",
// mirroring funcNamePattern. Group 1 is the import path, group 2 the type name.
var typeNamePattern = regexp.MustCompile(`^(.+)\.([^\d\W]\w*)$`)

// NewInstLitRule loads and validates an InstLitRule from YAML data.
func NewInstLitRule(data []byte, name string) (*InstLitRule, error) {
	var r InstLitRule
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, ex.Wrap(err)
	}
	if r.Name == "" {
		r.Name = name
	}

	// Parse the qualified type name once at creation.
	matches := typeNamePattern.FindStringSubmatch(r.StructLiteral)
	if matches == nil {
		return nil, ex.Newf("invalid struct_literal format: %q (expected 'package/path.TypeName')", r.StructLiteral)
	}
	r.ImportPath = matches[1]
	r.TypeName = matches[2]

	if err := r.validate(); err != nil {
		return nil, ex.Wrapf(err, "invalid literal rule %q", name)
	}
	return &r, nil
}

func (r *InstLitRule) validate() error {
	// StructLiteral format already validated in NewInstLitRule.
	if strings.TrimSpace(r.StructLiteral) == "" {
		return ex.Newf("struct_literal cannot be empty")
	}
	if len(r.Field) == 0 {
		return ex.Newf("field must list at least one field to set")
	}
	seen := make(map[string]bool, len(r.Field))
	for i, f := range r.Field {
		if strings.TrimSpace(f.Name) == "" {
			return ex.Newf("field[%d].name cannot be empty", i)
		}
		hasValue := strings.TrimSpace(f.Value) != ""
		hasWrap := strings.TrimSpace(f.Wrap) != ""
		if !hasValue && !hasWrap {
			return ex.Newf("field[%d] must set one of value or wrap", i)
		}
		if hasWrap && !replacePlaceholderPattern.MatchString(f.Wrap) {
			return ex.Newf(
				"field[%d].wrap must contain {{ . }} placeholder (also accepts {{.}}, {{- . -}}, etc.)", i,
			)
		}
		if seen[f.Name] {
			return ex.Newf("field %q is set more than once", f.Name)
		}
		seen[f.Name] = true
	}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler to ensure derived fields are
// populated after JSON deserialization, which is how the rule set crosses from
// the setup phase to the instrument phase.
func (r *InstLitRule) UnmarshalJSON(data []byte) error {
	// Use a type alias to avoid infinite recursion
	type Alias InstLitRule
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(r),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if r.ImportPath == "" || r.TypeName == "" {
		matches := typeNamePattern.FindStringSubmatch(r.StructLiteral)
		if matches == nil {
			return ex.Newf("invalid struct_literal format: %q", r.StructLiteral)
		}
		r.ImportPath = matches[1]
		r.TypeName = matches[2]
	}

	return nil
}
