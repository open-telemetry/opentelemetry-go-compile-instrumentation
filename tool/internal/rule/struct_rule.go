// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"gopkg.in/yaml.v3"
)

// InstStructField represents a single field to be added to a struct.
type InstStructField struct {
	Name string `json:"name" yaml:"name"` // The name of the field to be added
	Type string `json:"type" yaml:"type"` // The type of the field to be added
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

	Struct   string             `json:"struct"    yaml:"struct"`    // The type name of the struct to be instrumented
	NewField []*InstStructField `json:"new_field" yaml:"new_field"` // The new fields to be added
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
	return nil
}
