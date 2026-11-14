// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"maps"
	"slices"
	"strings"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// suppressHooksImport is the package toolexec imports into instrumented
// packages when the build uses //otelc:ignore.
const suppressHooksImport = "runtime"

// ruleSetAddedImports returns the imports toolexec can add to set's target
// package: every rule's declared imports, the imports of its add_file sources,
// and the //otelc:ignore support package when usesIgnore is set. The go
// command must see these before the build, or they are missing from the
// importcfg of the package. Call and literal rules only change files that
// import the package they match, so their imports carry that package in
// requires.
func (sp *setupPhase) ruleSetAddedImports(set *rule.InstRuleSet, usesIgnore bool) []ruleImport {
	seen := make(map[ruleImport]bool)
	add := func(imports map[string]string, requires string) {
		for _, path := range imports {
			if path == set.ModulePath || path == "unsafe" || path == "C" {
				continue
			}
			seen[ruleImport{path: path, requires: requires}] = true
		}
	}
	declared := func(r interface{ Base() *rule.InstBaseRule }) (map[string]string, string) {
		return r.Base().Imports, ""
	}
	addRuleImports(set.RawRules, add, func(r *rule.InstRawRule) (map[string]string, string) { return declared(r) })
	addRuleImports(set.FuncRules, add, func(r *rule.InstFuncRule) (map[string]string, string) { return declared(r) })
	addRuleImports(set.StructRules, add, func(r *rule.InstStructRule) (map[string]string, string) { return declared(r) })
	addRuleImports(set.DirectiveRules, add, func(r *rule.InstDirectiveRule) (map[string]string, string) {
		return declared(r)
	})
	addRuleImports(set.DeclRules, add, func(r *rule.InstDeclRule) (map[string]string, string) { return declared(r) })
	addRuleImports(set.CallRules, add, func(r *rule.InstCallRule) (map[string]string, string) {
		return r.Imports, r.ImportPath
	})
	addRuleImports(set.LitRules, add, func(r *rule.InstLitRule) (map[string]string, string) {
		return r.Imports, r.ImportPath
	})
	for _, r := range set.FileRules {
		imps, err := sp.fileRuleSourceImports(r)
		if err != nil {
			sp.Warn("could not inspect file rule source imports", "rule", r.Name, "error", err)
			continue
		}
		for _, imp := range imps {
			add(map[string]string{"": imp}, "")
		}
	}
	if usesIgnore {
		add(map[string]string{"": suppressHooksImport}, "")
	}
	return slices.SortedFunc(maps.Keys(seen), func(a, b ruleImport) int {
		if c := strings.Compare(a.path, b.path); c != 0 {
			return c
		}
		return strings.Compare(a.requires, b.requires)
	})
}

// anyIgnoredCallFiles reports whether any set holds a //otelc:ignore call.
func anyIgnoredCallFiles(sets []*rule.InstRuleSet) bool {
	for _, set := range sets {
		if len(set.IgnoredCallFiles) > 0 {
			return true
		}
	}
	return false
}
