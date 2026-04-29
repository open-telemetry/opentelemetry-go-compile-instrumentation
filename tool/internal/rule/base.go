// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// InstRule defines the interface for an instrumentation rule. Each rule
// specifies a target module and version, and has a unique name. The version
// range is optional and is used to filter rules that are applicable to the
// target module version. If the version is not specified, the rule is applicable
// to all versions of the target module. The left bound is inclusive, the right
// bound is exclusive. For example, "v1.0.0,v2.0.0" means the rule is applicable
// to the target module version range [v1.0.0, v2.0.0).
type InstRule interface {
	String() string      // The string representation of the rule
	GetName() string     // The unique name of the rule
	GetTarget() string   // The target module path where the rule is applied
	GetVersion() string  // The version range of target module if available, e.g "v1.0.0,v2.0.0"
	GetWhere() *WhereDef // Optional non-package selectors that remain after normalization
}

// FilterDef describes file predicates nested under where.file.
//
// The file predicate model currently supports implicit all-of across top-level
// fields plus the explicit qualifier keys needed by the agreed surface. Runtime
// support remains intentionally narrow: simple leaf predicates are supported,
// while qualifier composition is validated but not yet executed.
type FilterDef struct {
	AllOf []FilterDef `json:"all-of,omitempty" yaml:"all-of,omitempty"`
	OneOf []FilterDef `json:"one-of,omitempty" yaml:"one-of,omitempty"`
	Not   *FilterDef  `json:"not,omitempty"    yaml:"not,omitempty"`

	HasFunc      string `json:"has_func,omitempty"      yaml:"has_func,omitempty"`
	HasRecv      string `json:"recv,omitempty"          yaml:"recv,omitempty"`
	HasStruct    string `json:"has_struct,omitempty"    yaml:"has_struct,omitempty"`
	HasDirective string `json:"has_directive,omitempty" yaml:"has_directive,omitempty"`
}

// WhereDef carries the structured where clause after package selectors have
// been split back out to top-level target/version fields.
//
// Today the setup phase only executes where.file predicates. The remaining
// selector and qualifier fields are preserved here so the agreed syntax surface
// can be normalized now without forcing the broader internal refactor yet.
type WhereDef struct {
	File *FilterDef `json:"file,omitempty" yaml:"file,omitempty"`

	AllOf []WhereDef `json:"all-of,omitempty" yaml:"all-of,omitempty"`
	OneOf []WhereDef `json:"one-of,omitempty" yaml:"one-of,omitempty"`
	Not   *WhereDef  `json:"not,omitempty"    yaml:"not,omitempty"`

	Func         string `json:"func,omitempty"          yaml:"func,omitempty"`
	Recv         string `json:"recv,omitempty"          yaml:"recv,omitempty"`
	Struct       string `json:"struct,omitempty"        yaml:"struct,omitempty"`
	FunctionCall string `json:"function_call,omitempty" yaml:"function_call,omitempty"`
	Directive    string `json:"directive,omitempty"     yaml:"directive,omitempty"`
	Kind         string `json:"kind,omitempty"          yaml:"kind,omitempty"`
	Identifier   string `json:"identifier,omitempty"    yaml:"identifier,omitempty"`
}

// InstBaseRule is the base rule for all instrumentation rules.
type InstBaseRule struct {
	Name    string            `json:"name,omitempty"    yaml:"name,omitempty"`
	Target  string            `json:"target"            yaml:"target"`
	Version string            `json:"version,omitempty" yaml:"version,omitempty"`
	Imports map[string]string `json:"imports,omitempty" yaml:"imports,omitempty"` // map[alias]path
	Where   *WhereDef         `json:"where,omitempty"   yaml:"where,omitempty"`
}

func (ibr *InstBaseRule) String() string      { return ibr.Name }
func (ibr *InstBaseRule) GetName() string     { return ibr.Name }
func (ibr *InstBaseRule) GetTarget() string   { return ibr.Target }
func (ibr *InstBaseRule) GetVersion() string  { return ibr.Version }
func (ibr *InstBaseRule) GetWhere() *WhereDef { return ibr.Where }

// InstRuleSet represents a collection of instrumentation rules that apply to a
// single Go package within a specific module. It acts as a container for rules,
// organizing them by file and by the specific functions or structs they target.
// This structure is essential for the instrumentation process, as it allows the
// tool to efficiently locate and apply the correct rules to the source code.
type InstRuleSet struct {
	PackageName    string                          `json:"package_name"`
	ModulePath     string                          `json:"module_path"`
	CgoFileMap     map[string]string               `json:"cgo_file_map,omitempty"` // go -> cgo
	RawRules       map[string][]*InstRawRule       `json:"raw_rules"`
	FuncRules      map[string][]*InstFuncRule      `json:"func_rules"`
	StructRules    map[string][]*InstStructRule    `json:"struct_rules"`
	CallRules      map[string][]*InstCallRule      `json:"call_rules"`
	DirectiveRules map[string][]*InstDirectiveRule `json:"directive_rules"`
	DeclRules      map[string][]*InstDeclRule      `json:"decl_rules"`
	FileRules      []*InstFileRule                 `json:"file_rules"`
}

func NewInstRuleSet(importPath string) *InstRuleSet {
	return &InstRuleSet{
		PackageName:    "",
		ModulePath:     importPath,
		CgoFileMap:     make(map[string]string),
		RawRules:       make(map[string][]*InstRawRule),
		FuncRules:      make(map[string][]*InstFuncRule),
		StructRules:    make(map[string][]*InstStructRule),
		CallRules:      make(map[string][]*InstCallRule),
		DirectiveRules: make(map[string][]*InstDirectiveRule),
		DeclRules:      make(map[string][]*InstDeclRule),
		FileRules:      make([]*InstFileRule, 0),
	}
}

func (irs *InstRuleSet) String() string {
	parts := []string{
		fmt.Sprintf("raw=%v", irs.RawRules),
		fmt.Sprintf("func=%v", irs.FuncRules),
		fmt.Sprintf("struct=%v", irs.StructRules),
		fmt.Sprintf("call=%v", irs.CallRules),
		fmt.Sprintf("directive=%v", irs.DirectiveRules),
		fmt.Sprintf("decl=%v", irs.DeclRules),
		fmt.Sprintf("file=%v", irs.FileRules),
	}
	return fmt.Sprintf("{%s: %s}", irs.ModulePath, strings.Join(parts, ", "))
}

func (irs *InstRuleSet) IsEmpty() bool {
	return irs == nil ||
		(len(irs.FuncRules) == 0 &&
			len(irs.StructRules) == 0 &&
			len(irs.RawRules) == 0 &&
			len(irs.CallRules) == 0 &&
			len(irs.DirectiveRules) == 0 &&
			len(irs.DeclRules) == 0 &&
			len(irs.FileRules) == 0)
}

// AddRule is a generic method that adds any type of rule to the appropriate map.
// It works with any rule type that implements the InstRule interface.
func addRule[T InstRule](file string, rule T, rulesMap map[string][]T) {
	util.Assert(filepath.IsAbs(file), "file must be an absolute path")
	rulesMap[file] = append(rulesMap[file], rule)
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

func (irs *InstRuleSet) AddCallRule(file string, rule *InstCallRule) {
	addRule(file, rule, irs.CallRules)
}

func (irs *InstRuleSet) AddDirectiveRule(file string, rule *InstDirectiveRule) {
	addRule(file, rule, irs.DirectiveRules)
}

func (irs *InstRuleSet) AddDeclRule(file string, rule *InstDeclRule) {
	addRule(file, rule, irs.DeclRules)
}

func (irs *InstRuleSet) AddFileRule(rule *InstFileRule) {
	irs.FileRules = append(irs.FileRules, rule)
}

func (irs *InstRuleSet) SetPackageName(name string) {
	util.Assert(name != "", "package name is empty")
	irs.PackageName = name
}

// SetCgoFileMap sets the CGO file mapping for this rule set.
func (irs *InstRuleSet) SetCgoFileMap(cgoFiles map[string]string) {
	irs.CgoFileMap = cgoFiles
}

// AllFuncRules returns all function rules from the rule set as a flat slice.
func (irs *InstRuleSet) AllFuncRules() []*InstFuncRule {
	n := 0
	for _, rs := range irs.FuncRules {
		n += len(rs)
	}
	rules := make([]*InstFuncRule, 0, n)
	for _, rs := range irs.FuncRules {
		rules = append(rules, rs...)
	}
	return rules
}

// AllStructRules returns all struct rules from the rule set as a flat slice.
func (irs *InstRuleSet) AllStructRules() []*InstStructRule {
	n := 0
	for _, rs := range irs.StructRules {
		n += len(rs)
	}
	rules := make([]*InstStructRule, 0, n)
	for _, rs := range irs.StructRules {
		rules = append(rules, rs...)
	}
	return rules
}
