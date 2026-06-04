// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"fmt"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"gopkg.in/yaml.v3"
)

// FuncSignature specifies the argument and result types used by signature
// sub-filters on InstFuncRule.  Each entry is a type name string in the form
// accepted by the type-name parser (e.g. "error", "context.Context",
// "*http.Request").  For exact matching (signature), the i-th entry is
// compared to the i-th field in order.  For contains matching
// (signature_contains), each entry is checked for presence anywhere in the
// corresponding list.
type FuncSignature struct {
	Args    []string `json:"args,omitempty"    yaml:"args"`
	Returns []string `json:"returns,omitempty" yaml:"returns"`
}

// InstFuncRule represents a rule that guides hook function injection into
// appropriate target function locations. For example, if we want to inject
// custom Foo function at the entry of target function Bar, we can define a rule:
//
//	rule:
//		name: "newrule"
//		target: "main"
//		func: "Bar"
//		recv: "*RecvType"
//		before: "Foo"
//		path: "github.com/foo/bar/hook_rule"
//
// Optional signature sub-filters narrow matching beyond name and receiver:
//
//	signature:
//	  args: [context.Context, string]
//	  returns: [error]
//	signature_contains:
//	  args: [context.Context]
//	result: error
//	last_result: error
//	param: context.Context
type InstFuncRule struct {
	InstBaseRule `yaml:",inline"`

	Func   string `json:"func"   yaml:"func"`   // The name of the target func to be instrumented
	Recv   string `json:"recv"   yaml:"recv"`   // The name of the receiver type
	Before string `json:"before" yaml:"before"` // The function we inject at the target function entry
	After  string `json:"after"  yaml:"after"`  // The function we inject at the target function exit
	Path   string `json:"path"   yaml:"path"`   // The module path where hook code is located

	// Optional signature sub-filters (all non-empty filters must match; combined
	// with AND logic so any combination is allowed).
	Signature         *FuncSignature `json:"signature,omitempty"          yaml:"signature"`
	SignatureContains *FuncSignature `json:"signature_contains,omitempty" yaml:"signature_contains"`
	Result            string         `json:"result,omitempty"             yaml:"result"`
	LastResult        string         `json:"last_result,omitempty"        yaml:"last_result"`
	Param             string         `json:"param,omitempty"              yaml:"param"`

	// ApplicationIndex is the zero-based position of this rule within a do
	// sequence (see rule.Normalize): it counts which application of a modifier to
	// the same target this rule represents. It is not part of the user-facing
	// schema; it exists solely so that String — and therefore the generated
	// trampoline names — stays unique when several modifiers target the same
	// function. Index 0 is the default and contributes no suffix, preserving the
	// names of single-modifier and legacy rules.
	ApplicationIndex int `json:"application_index,omitempty" yaml:"application_index,omitempty"`
}

// NewInstFuncRule loads and validates an InstFuncRule from YAML data.
func NewInstFuncRule(data []byte, name string) (*InstFuncRule, error) {
	var r InstFuncRule
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, ex.Wrap(err)
	}
	if r.Name == "" {
		r.Name = name
	}
	if err := r.validate(); err != nil {
		return nil, ex.Wrapf(err, "invalid func rule %q", name)
	}
	return &r, nil
}

func (r *InstFuncRule) validate() error {
	if strings.TrimSpace(r.Func) == "" {
		return ex.Newf("func cannot be empty")
	}
	if strings.TrimSpace(r.Before) == "" && strings.TrimSpace(r.After) == "" {
		return ex.Newf("before or after must be set")
	}
	return nil
}

// applicationIndexSep separates a rule name from its application-index suffix in
// String. It is intentionally a character that cannot appear in a rule name
// (names are identifier-like YAML keys such as "wrap_default_transport"), so a
// suffixed identity like "open_rule#1" can never collide with a distinct rule
// literally named "open_rule_1" carrying application index 0 (issue #560).
const applicationIndexSep = "#"

// String returns a stable identity for the rule used to derive trampoline and
// HookContext names. It must differ between two rules that target the same
// function via separate do-sequence modifiers, otherwise their generated
// declarations collide. The application-index suffix is only appended for
// index > 0 so that single-modifier and legacy rules keep their historical
// names; the reserved separator (applicationIndexSep) guarantees the suffixed
// form cannot collide with a user-chosen rule name.
func (r *InstFuncRule) String() string {
	if r.ApplicationIndex == 0 {
		return r.Name
	}
	return fmt.Sprintf("%s%s%d", r.Name, applicationIndexSep, r.ApplicationIndex)
}
