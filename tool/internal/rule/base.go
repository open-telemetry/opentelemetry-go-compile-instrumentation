// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"path/filepath"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

type InstRule interface {
	String() string    // The string representation of the rule
	GetName() string   // The name of the rule
	GetTarget() string // The target module path where the rule is applied
}

// InstBaseRule is the base rule for all instrumentation rules.
type InstBaseRule struct {
	Name   string `json:"name,omitempty" yaml:"name,omitempty"` // The name of the rule
	Target string `json:"target"         yaml:"target"`         // The target module path where the rule is applied
}

func (ibr *InstBaseRule) String() string    { return ibr.Name }
func (ibr *InstBaseRule) GetName() string   { return ibr.Name }
func (ibr *InstBaseRule) GetTarget() string { return ibr.Target }

// InstRuleSet represents a collection of instrumentation rules that apply to a
// single Go package within a specific module. It acts as a container for rules,
// organizing them by file and by the specific functions or structs they target.
// This structure is essential for the instrumentation process, as it allows the
// tool to efficiently locate and apply the correct rules to the source code.
type InstRuleSet struct {
	PackageName string                       `json:"package_name"`
	ModulePath  string                       `json:"module_path"`
	RawRules    map[string][]*InstRawRule    `json:"raw_rules"`
	FuncRules   map[string][]*InstFuncRule   `json:"func_rules"`
	StructRules map[string][]*InstStructRule `json:"struct_rules"`
	FileRules   []*InstFileRule              `json:"file_rules"`
}

func NewInstRuleSet(importPath string) *InstRuleSet {
	return &InstRuleSet{
		PackageName: "",
		ModulePath:  importPath,
		RawRules:    make(map[string][]*InstRawRule),
		FuncRules:   make(map[string][]*InstFuncRule),
		StructRules: make(map[string][]*InstStructRule),
		FileRules:   make([]*InstFileRule, 0),
	}
}

func (irs *InstRuleSet) IsEmpty() bool {
	return irs == nil ||
		(len(irs.FuncRules) == 0 &&
			len(irs.StructRules) == 0 &&
			len(irs.RawRules) == 0 &&
			len(irs.FileRules) == 0)
}

// AddRule is a generic method that adds any type of rule to the appropriate map.
// It works with any rule type that implements the InstRule interface.
func addRule[T InstRule](file string, rule T, rulesMap map[string][]T) {
	util.Assert(filepath.IsAbs(file), "file must be an absolute path")
	if _, exist := rulesMap[file]; !exist {
		rulesMap[file] = make([]T, 0)
		rulesMap[file] = append(rulesMap[file], rule)
	} else {
		rulesMap[file] = append(rulesMap[file], rule)
	}
}

func (irs *InstRuleSet) AddRawRule(file string, rule *InstRawRule) {
	addRule(file, rule, irs.RawRules)
}

func (irs *InstRuleSet) AddFuncRule(file string, rule *InstFuncRule) {
	addRule(file, rule, irs.FuncRules)
}

func (irs *InstRuleSet) AddStructRule(file string, rule *InstStructRule) {
	addRule(file, rule, irs.StructRules)
}

func (irs *InstRuleSet) AddFileRule(rule *InstFileRule) {
	irs.FileRules = append(irs.FileRules, rule)
}

func (irs *InstRuleSet) SetPackageName(name string) {
	util.Assert(name != "", "package name is empty")
	irs.PackageName = name
}

// GetFuncRules returns all function rules from the rule set.
// This is a convenience method that uses the generic GetRules function.
func (irs *InstRuleSet) GetFuncRules() []*InstFuncRule {
	rules := make([]*InstFuncRule, 0)
	for _, rs := range irs.FuncRules {
		rules = append(rules, rs...)
	}
	return rules
}

// GetStructRules returns all struct rules from the rule set.
// This is a convenience method that uses the generic GetRules function.
func (irs *InstRuleSet) GetStructRules() []*InstStructRule {
	rules := make([]*InstStructRule, 0)
	for _, rs := range irs.StructRules {
		rules = append(rules, rs...)
	}
	return rules
}
