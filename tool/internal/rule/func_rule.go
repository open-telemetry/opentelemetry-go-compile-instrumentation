// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strconv"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
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

	Func       string `json:"func"   yaml:"func"`   // The name of the target func to be instrumented
	Recv       string `json:"recv"   yaml:"recv"`   // The name of the receiver type
	Before     string `json:"before" yaml:"before"` // The function we inject at the target function entry
	After      string `json:"after"  yaml:"after"`  // The function we inject at the target function exit
	Path       string `json:"path"   yaml:"path"`   // The import path where hook code is located
	ModulePath string `json:"-"      yaml:"module"` // The module path where hook code is located

	ResolvedPath string `json:"resolved_path" yaml:"-"` // The local path of the package directory resolved from import path

	// Optional signature sub-filters (all non-empty filters must match; combined
	// with AND logic so any combination is allowed).
	Signature         *FuncSignature `json:"signature,omitempty"          yaml:"signature"`
	SignatureContains *FuncSignature `json:"signature_contains,omitempty" yaml:"signature_contains"`
	Result            string         `json:"result,omitempty"             yaml:"result"`
	LastResult        string         `json:"last_result,omitempty"        yaml:"last_result"`
	Param             string         `json:"param,omitempty"              yaml:"param"`
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
	if r.ModulePath == "" {
		r.ModulePath = r.Path
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
	if strings.TrimSpace(r.Path) == "" {
		return ex.Newf("path cannot be empty")
	}
	if r.Path != r.ModulePath && !strings.HasPrefix(r.Path, r.ModulePath+"/") {
		return ex.Newf("import path %q is not part of module path %q", r.Path, r.ModulePath)
	}
	return nil
}

// Identity returns a content-derived key used to generate trampoline and
// HookContext names. It is a function purely of what the rule does — its
// target, function/receiver, before/after hooks, hook path, and signature
// filters — never of the rule's name or its position in a do sequence.
//
// De-duplication: two rules that do the same thing share an identity, so they
// collapse to a single generated artifact instead of redeclaring it. The only
// remaining collision is between byte-identical rules, which are effectively the
// same rule. A rule's position in a do sequence is intentionally excluded — it
// does not change what a rule does; do-sequence order is preserved by the order
// in which the expanded rules are applied, not by the generated name.
//
// Deriving the identity from content (rather than a "name#index" string) closes
// the collision in issue #560, where a rule literally named "name#index" at
// application index 0 rendered the same string as "name" at index N.
//
// When adding a field to InstFuncRule that changes the generated instrumentation,
// include it here so the identity stays faithful to what the rule does.
//
// The key is built with explicit length prefixes ("len:value") instead of a
// delimiter, so it is injective for arbitrary field content. Type-name strings
// may legitimately contain any character — commas in function types, "|" in
// type constraints — so no printable separator is truly reserved; the length
// prefix marks where each value ends without relying on its content.
func (r *InstFuncRule) Identity() string {
	enc := func(s string) string { return strconv.Itoa(len(s)) + ":" + s }
	encList := func(xs []string) string {
		encs := make([]string, len(xs))
		for i, x := range xs {
			encs[i] = enc(x)
		}
		return strconv.Itoa(len(xs)) + ";" + strings.Join(encs, "")
	}
	encSig := func(s *FuncSignature) string {
		if s == nil {
			return "-" // absent: distinct from a present-but-empty signature
		}
		return "+" + encList(s.Args) + encList(s.Returns)
	}
	parts := []string{
		enc(r.Target), enc(r.Version), enc(r.Func), enc(r.Recv),
		enc(r.Before), enc(r.After), enc(r.Path),
		enc(r.Result), enc(r.LastResult), enc(r.Param),
		encSig(r.Signature), encSig(r.SignatureContains),
	}
	return util.CRC32(strings.Join(parts, ""))
}
