// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"gopkg.in/yaml.v3"
)

// InstRawRule represents a rule that allows raw Go source code injection into
// appropriate target function locations. For example, if we want to inject
// raw code at the entry of target function Bar, we can define a rule:
//
//	rule:
//		name: "newrule"
//		target: "main"
//		func: "Bar"
//		recv: "*Recv"
//		raw: "println(\"Hello, World!\")"
type InstRawRule struct {
	InstBaseRule `yaml:",inline"`

	Func string `json:"func" yaml:"func"` // The name of the target func to be instrumented
	Recv string `json:"recv" yaml:"recv"` // The name of the receiver type
	Raw  string `json:"raw"  yaml:"raw"`  // The raw code to be injected
}

// NewInstRawRule loads and validates an InstRawRule from YAML data.
func NewInstRawRule(data []byte, name string) (*InstRawRule, error) {
	var r InstRawRule
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, ex.Wrap(err)
	}
	if r.Name == "" {
		r.Name = name
	}
	if err := r.validate(); err != nil {
		return nil, ex.Wrapf(err, "invalid raw rule %q", name)
	}
	return &r, nil
}

func (r *InstRawRule) validate() error {
	if strings.TrimSpace(r.Raw) == "" {
		return ex.Newf("raw cannot be empty")
	}
	return nil
}
