// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"regexp"
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
//		pattern: "^name := getName\\(\\)$"
//		placement: before|after
type InstRawRule struct {
	InstBaseRule `yaml:",inline"`

	Func      string `json:"func"                yaml:"func"`                // The name of the target func to be instrumented
	Recv      string `json:"recv"                yaml:"recv"`                // The name of the receiver type
	Raw       string `json:"raw"                 yaml:"raw"`                 // The raw code to be injected
	Pattern   string `json:"pattern,omitempty"   yaml:"pattern,omitempty"`   // The position to inject the raw code. Must be a regex pattern
	Placement string `json:"placement,omitempty" yaml:"placement,omitempty"` // The placement of the raw code. Can be "before" or "after". Default is "before".
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
	if _, err := regexp.Compile(r.Pattern); err != nil {
		return ex.Wrapf(err, "invalid regex pattern for raw rule: %q", r.Pattern)
	}
	if r.Placement != "" && r.Placement != "before" && r.Placement != "after" {
		return ex.Newf("invalid placement value: %q, must be 'before' or 'after'", r.Placement)
	}
	return nil
}
