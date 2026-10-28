// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.yaml.in/yaml/v3"
)

// InstStructField represents a single field to be added to a struct.
type InstStructField struct {
	Name string `json:"name" yaml:"name"`
	Type string `json:"type" yaml:"type"`
}

// InstStructRule represents a rule that adds new fields to a target struct.
// For example, to inject a custom field into a struct:
//
//	add_new_field:
//	  target: main
//	  struct: MyStruct
//	  new_field:
//	    - name: NewField
//	      type: string
type InstStructRule struct {
	InstBaseRule `yaml:",inline"`

	Struct   string             `json:"struct"    yaml:"struct"`
	NewField []*InstStructField `json:"new_field" yaml:"new_field"`
}

// NewInstStructRule loads and validates an InstStructRule from YAML data.
func NewInstStructRule(data []byte, name string) (*InstStructRule, error) {
	var r InstStructRule
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, ex.Wrap(err)
	}
	if r.Name == "" {
		r.Name = name
	}
	if err := r.validate(); err != nil {
		return nil, ex.Wrapf(err, "invalid struct rule %q", name)
	}
	return &r, nil
}

func (r *InstStructRule) validate() error {
	if strings.TrimSpace(r.Struct) == "" {
		return ex.Newf("struct cannot be empty")
	}
	seen := make(map[string]bool, len(r.NewField))
	for i, f := range r.NewField {
		if f == nil {
			return ex.Newf("new_field[%d] cannot be empty", i)
		}
		if strings.TrimSpace(f.Name) == "" {
			return ex.Newf("new_field[%d].name cannot be empty", i)
		}
		if strings.TrimSpace(f.Type) == "" {
			return ex.Newf("new_field[%d].type cannot be empty", i)
		}
		if seen[f.Name] {
			return ex.Newf("new_field %q is added more than once", f.Name)
		}
		seen[f.Name] = true
	}
	return nil
}
