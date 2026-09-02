// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"regexp"
	"strconv"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
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

	Func      string `json:"func"                yaml:"func"`
	Recv      string `json:"recv"                yaml:"recv"`
	Raw       string `json:"raw"                 yaml:"raw"`
	Pattern   string `json:"pattern,omitempty"   yaml:"pattern,omitempty"`
	Placement string `json:"placement,omitempty" yaml:"placement,omitempty"`
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
	// raw is only treated as a template when it contains "{{"
	if strings.Contains(r.Raw, "{{") {
		if _, err := ParseFuncTemplate(r.Raw); err != nil {
			return ex.Wrapf(err, "invalid template syntax in raw")
		}
	}
	if _, err := regexp.Compile(r.Pattern); err != nil {
		return ex.Wrapf(err, "invalid regex pattern for raw rule: %q", r.Pattern)
	}
	if r.Placement != "" && r.Placement != "before" && r.Placement != "after" {
		return ex.Newf("invalid placement value: %q, must be 'before' or 'after'", r.Placement)
	}
	return nil
}

// Identity returns a content-derived key used to salt the synthetic argument
// and return-value names FuncArgument/FuncReturn assign (see
// collectArguments/collectReturnValues), the same way
// InstDirectiveRule.Identity salts directive-rule template names (issue
// #560, PR #1035). It is a function purely of what the rule does — its
// target, version, func, and raw code — never of the rule's name.
func (r *InstRawRule) Identity() string {
	enc := func(s string) string { return strconv.Itoa(len(s)) + ":" + s }
	parts := []string{enc(r.Target), enc(r.Version), enc(r.Func), enc(r.Recv), enc(r.Raw)}
	return util.CRC32(strings.Join(parts, ""))
}
