// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// compileKey identifies the compiles of one import path, either the test or
// the normal ones.
type compileKey struct {
	importPath string
	isTest     bool
}

// mergeRuleSets returns matched with one rule set for each package compile.
// The instrument phase applies only the first rule set that matches a compile,
// so a second set for the same compile is never applied. Setup can match two
// sets for one compile: matchRuntimeGLS adds otelc's own set for runtime after
// the set that the rules for runtime matched.
//
// The sets of one compile are merged into one set, at the position of the
// first one, with the rules in the order of matched. A build can compile
// several packages with one import path, such as its main packages, and each
// of them has its own set. deps holds the compiles of the build plan, and the
// sets of an import path that deps has more than once are kept as they are.
func mergeRuleSets(matched []*rule.InstRuleSet, deps []*Dependency) ([]*rule.InstRuleSet, error) {
	groups := make(map[compileKey][]*rule.InstRuleSet, len(matched))
	for _, set := range matched {
		key := compileKey{importPath: set.ModulePath, isTest: set.IsTest}
		groups[key] = append(groups[key], set)
	}
	compiles := make(map[compileKey]int, len(deps))
	for _, dep := range deps {
		compiles[compileKey{importPath: dep.ImportPath, isTest: dep.IsTest}]++
	}

	merged := make([]*rule.InstRuleSet, 0, len(matched))
	for _, set := range matched {
		key := compileKey{importPath: set.ModulePath, isTest: set.IsTest}
		group := groups[key]
		if len(group) == 1 || compiles[key] > 1 {
			merged = append(merged, set)
			continue
		}
		if group[0] != set {
			continue
		}
		one := rule.NewInstRuleSet(set.ModulePath)
		one.IsTest = set.IsTest
		for _, member := range group {
			if err := one.Merge(member); err != nil {
				return nil, err
			}
		}
		merged = append(merged, one)
	}
	return merged, nil
}
