// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"gopkg.in/yaml.v3"
)

// InstValueDeclRule represents a rule that matches package-level var/const
// declarations by their declared type and replaces the declaration value.
//
// Only declarations with an explicit type annotation are matched. Declarations
// like `const x = true` (untyped) are skipped.
//
// Example YAML:
//
//	replace_feature_flag:
//	  target: mypackage
//	  value_declaration: "bool"
//	  assign_value: "true"
//
// Supported type formats:
//   - "bool", "string", "int" — built-in types
//   - "net/http.Client" — qualified type (full import path + type name)
//   - "*net/http.Request" — pointer to qualified type
type InstValueDeclRule struct {
	InstBaseRule `yaml:",inline"`

	ValueDeclaration string `json:"value_declaration" yaml:"value_declaration"`
	AssignValue      string `json:"assign_value"      yaml:"assign_value"`

	// Derived fields (parsed from ValueDeclaration during construction)
	TypeImportPath string `json:"type_import_path" yaml:"-"`
	TypeIdent      string `json:"type_ident"       yaml:"-"`
	TypePointer    bool   `json:"type_pointer"     yaml:"-"`
}

// valueDeclTypePattern matches type expressions for value_declaration.
//
//   - Group 1 (optional): "*" for pointer types
//   - Group 2 (optional): import path (e.g., "net/http")
//   - Group 3 (required): type name (e.g., "bool", "Client")
var valueDeclTypePattern = regexp.MustCompile(
	`^(\*)?(?:([A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*)\.)?([A-Za-z_][A-Za-z0-9_]*)$`,
)

// NewInstValueDeclRule loads and validates an InstValueDeclRule from YAML data.
func NewInstValueDeclRule(data []byte, name string) (*InstValueDeclRule, error) {
	var r InstValueDeclRule
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, ex.Wrap(err)
	}
	if r.Name == "" {
		r.Name = name
	}
	if err := r.parseAndValidate(); err != nil {
		return nil, ex.Wrapf(err, "invalid value_decl rule %q", name)
	}
	return &r, nil
}

func (r *InstValueDeclRule) parseAndValidate() error {
	if strings.TrimSpace(r.ValueDeclaration) == "" {
		return ex.Newf("value_declaration cannot be empty")
	}
	if strings.TrimSpace(r.AssignValue) == "" {
		return ex.Newf("assign_value cannot be empty")
	}
	return r.parseTypeDeclaration()
}

// parseTypeDeclaration parses ValueDeclaration and populates the derived type fields.
func (r *InstValueDeclRule) parseTypeDeclaration() error {
	matches := valueDeclTypePattern.FindStringSubmatch(r.ValueDeclaration)
	if matches == nil {
		return ex.Newf("invalid value_declaration format: %q", r.ValueDeclaration)
	}
	r.TypePointer = matches[1] == "*"
	r.TypeImportPath = matches[2]
	r.TypeIdent = matches[3]
	return nil
}

// UnmarshalJSON implements json.Unmarshaler to ensure derived fields are
// populated after JSON deserialization (e.g., when loading matched rules from
// the matched-rules JSON file written by the setup phase).
func (r *InstValueDeclRule) UnmarshalJSON(data []byte) error {
	type Alias InstValueDeclRule
	aux := &struct{ *Alias }{Alias: (*Alias)(r)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	// Repopulate derived fields if they were not persisted.
	if r.TypeIdent == "" && r.ValueDeclaration != "" {
		return r.parseTypeDeclaration()
	}
	return nil
}
