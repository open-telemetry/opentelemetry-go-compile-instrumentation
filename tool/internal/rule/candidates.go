// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import "go.opentelemetry.io/otelc/tool/util"

// InstRuleCandidates is the package-level view of rules that may apply to one
// import path after target and version filtering. Unlike the file-keyed maps on
// InstRuleSet, entries are not keyed by absolute user-source paths.
//
// This is written next to the existing file-keyed maps in matched.json so
// toolexec can keep using those maps while later #552 steps switch file
// matching to the compile command.
type InstRuleCandidates struct {
	RawRules       []*InstRawRule       `json:"raw_rules,omitempty"`
	FuncRules      []*InstFuncRule      `json:"func_rules,omitempty"`
	StructRules    []*InstStructRule    `json:"struct_rules,omitempty"`
	CallRules      []*InstCallRule      `json:"call_rules,omitempty"`
	DirectiveRules []*InstDirectiveRule `json:"directive_rules,omitempty"`
	DeclRules      []*InstDeclRule      `json:"decl_rules,omitempty"`
	FileRules      []*InstFileRule      `json:"file_rules,omitempty"`
}

// SetCandidates records the target+version filtered rules for this package.
// Passing an empty slice clears Candidates so legacy matched.json stays compact.
func (irs *InstRuleSet) SetCandidates(rules []InstRule) {
	irs.Candidates = candidatesFromRules(rules)
}

func candidatesFromRules(rules []InstRule) *InstRuleCandidates {
	if len(rules) == 0 {
		return nil
	}
	c := &InstRuleCandidates{}
	for _, r := range rules {
		switch rt := r.(type) {
		case *InstRawRule:
			c.RawRules = append(c.RawRules, rt)
		case *InstFuncRule:
			c.FuncRules = append(c.FuncRules, rt)
		case *InstStructRule:
			c.StructRules = append(c.StructRules, rt)
		case *InstCallRule:
			c.CallRules = append(c.CallRules, rt)
		case *InstDirectiveRule:
			c.DirectiveRules = append(c.DirectiveRules, rt)
		case *InstDeclRule:
			c.DeclRules = append(c.DeclRules, rt)
		case *InstFileRule:
			c.FileRules = append(c.FileRules, rt)
		default:
			util.ShouldNotReachHere()
		}
	}
	return c
}
