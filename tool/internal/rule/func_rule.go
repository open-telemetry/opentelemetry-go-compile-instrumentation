// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
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
//	result_type: error
//	last_result_type: error
//	argument_type: context.Context
type InstFuncRule struct {
	InstBaseRule `yaml:",inline"`

	Func   string `json:"func"   yaml:"func"`   // The name of the target func to be instrumented
	Recv   string `json:"recv"   yaml:"recv"`   // The name of the receiver type
	Before string `json:"before" yaml:"before"` // The function we inject at the target function entry
	After  string `json:"after"  yaml:"after"`  // The function we inject at the target function exit
	Path   string `json:"path"   yaml:"path"`   // The module path where hook code is located

	// Optional signature sub-filters (all non-nil filters must match; combined
	// with AND logic so any combination is allowed).
	Signature         *FuncSignature `json:"signature,omitempty"          yaml:"signature"`
	SignatureContains *FuncSignature `json:"signature_contains,omitempty" yaml:"signature_contains"`
	ResultType        *string        `json:"result_type,omitempty"        yaml:"result_type"`
	LastResultType    *string        `json:"last_result_type,omitempty"   yaml:"last_result_type"`
	ArgumentType      *string        `json:"argument_type,omitempty"      yaml:"argument_type"`
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
