// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/data"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"gopkg.in/yaml.v3"
)

// CreateRuleFromFields creates a rule instance based on the field type present in the YAML
//
//nolint:nilnil // factory function
func CreateRuleFromFields(raw []byte, name string, fields map[string]any) (InstRule, error) {
	switch {
	case fields["struct"] != nil:
		return NewInstStructRule(raw, name)
	case fields["file"] != nil:
		return NewInstFileRule(raw, name)
	case fields["raw"] != nil:
		return NewInstRawRule(raw, name)
	case fields["func"] != nil:
		return NewInstFuncRule(raw, name)
	default:
		util.ShouldNotReachHere()
		return nil, nil
	}
}

// ParseEmbeddedRule parses the embedded yaml rule file to concrete rule instances
func ParseEmbeddedRule(path string) ([]InstRule, error) {
	yamlFile, err := data.ReadEmbedFile(path)
	if err != nil {
		return nil, err
	}
	var h map[string]map[string]any
	err = yaml.Unmarshal(yamlFile, &h)
	if err != nil {
		return nil, ex.Wrap(err)
	}
	rules := make([]InstRule, 0)
	for name, fields := range h {
		raw, err1 := yaml.Marshal(fields)
		if err1 != nil {
			return nil, ex.Wrap(err1)
		}

		r, err2 := CreateRuleFromFields(raw, name, fields)
		if err2 != nil {
			return nil, err2
		}
		rules = append(rules, r)
	}
	return rules, nil
}
