// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"gopkg.in/yaml.v3"
)

// InstStructRule represents a rule that guides hook struct field injection into
// appropriate target struct locations. For example, if we want to inject
// custom Foo field at the target struct Bar, we can define a rule:
//
//	rule:
//		name: "rule"
//		target: "main"
//		struct: "Bar"
//		field_name: "Foo"
//		field_type: "int"
type InstStructField struct {
	Name string `json:"name" yaml:"name"` // The name of the field to be added
	Type string `json:"type" yaml:"type"` // The type of the field to be added
}

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
