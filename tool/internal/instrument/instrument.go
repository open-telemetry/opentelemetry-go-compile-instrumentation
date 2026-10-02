// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"maps"
	"path/filepath"
	"slices"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// groupRules groups rset's rules by absolute file path, and also returns
// those paths in sorted order. instrument() returns on the first error it
// hits while walking the files it's given, so an unsorted order would make
// which failure gets reported (and the order of per-file log lines) vary
// between identical runs of the same input.
func groupRules(workDir string, rset *rule.InstRuleSet) (map[string][]rule.InstRule, []string) {
	file2rules := make(map[string][]rule.InstRule)
	addRulesToMap(rset.FuncRules, file2rules, rset.CgoFileMap, workDir)
	addRulesToMap(rset.StructRules, file2rules, rset.CgoFileMap, workDir)
	addRulesToMap(rset.RawRules, file2rules, rset.CgoFileMap, workDir)
	addRulesToMap(rset.CallRules, file2rules, rset.CgoFileMap, workDir)
	addRulesToMap(rset.LitRules, file2rules, rset.CgoFileMap, workDir)
	addRulesToMap(rset.DirectiveRules, file2rules, rset.CgoFileMap, workDir)
	addRulesToMap(rset.DeclRules, file2rules, rset.CgoFileMap, workDir)
	return file2rules, slices.Sorted(maps.Keys(file2rules))
}

func addRulesToMap[T rule.InstRule](
	source map[string][]T,
	file2rules map[string][]rule.InstRule,
	cgoMap map[string]string,
	workDir string,
) {
	for file, rules := range source {
		if cgoBase, ok := cgoMap[file]; ok {
			// CGO file path is always relative to the working directory
			file = filepath.Join(workDir, cgoBase)
		}
		for _, r := range rules {
			file2rules[file] = append(file2rules[file], r)
		}
	}
}

// ruleResult is what applying one rule to a file did.
type ruleResult struct {
	// needsGlobals reports whether the rule injected code that depends on the
	// globals file.
	needsGlobals bool
	// modified reports whether the rule changed the file. Call and literal
	// rules are attached to every file of a target package and change only the
	// files that contain a match; the other rule kinds only reach files setup
	// already matched.
	modified bool
}

// applyOneRule applies a single rule to the target file.
func (ip *instrumentPhase) applyOneRule(ctx context.Context, r rule.InstRule, root *dst.File) (ruleResult, error) {
	switch rt := r.(type) {
	case *rule.InstFuncRule:
		return ruleResult{needsGlobals: true, modified: true}, ip.applyFuncRule(ctx, rt, root)
	case *rule.InstStructRule:
		return ruleResult{needsGlobals: false, modified: true}, ip.applyStructRule(ctx, rt, root)
	case *rule.InstDeclRule:
		return ruleResult{needsGlobals: false, modified: true}, ip.applyDeclRule(ctx, rt, root)
	case *rule.InstRawRule:
		return ruleResult{needsGlobals: true, modified: true}, ip.applyRawRule(ctx, rt, root)
	case *rule.InstCallRule:
		modified, err := ip.applyCallRule(ctx, rt, root)
		return ruleResult{needsGlobals: false, modified: modified}, err
	case *rule.InstLitRule:
		modified, err := ip.applyLitRule(ctx, rt, root)
		return ruleResult{needsGlobals: false, modified: modified}, err
	case *rule.InstDirectiveRule:
		needsGlobals, err := ip.applyDirectiveRule(ctx, rt, root)
		return ruleResult{needsGlobals: needsGlobals, modified: true}, err
	default:
		util.ShouldNotReachHere()
		return ruleResult{needsGlobals: false, modified: false}, nil
	}
}

func (ip *instrumentPhase) instrument(ctx context.Context, rset *rule.InstRuleSet) error {
	hasFuncRule := false
	// Apply file rules first because they can introduce new files that used
	// by other rules such as raw rules
	for _, rule := range rset.FileRules {
		err := ip.applyFileRule(ctx, rule, rset.PackageName)
		if err != nil {
			return ex.Wrapf(err, "applying file rule %s to package %s", rule.Name, rset.PackageName)
		}
	}
	file2rules, files := groupRules(ip.workDir, rset)
	for _, file := range files {
		rules := file2rules[file]
		// Group rules by file, then parse the target file once
		root, err := ip.parseFile(file)
		if err != nil {
			return ex.Wrapf(err, "parsing file %s", file)
		}

		// Apply the rules to the target file
		modified := false
		for _, r := range rules {
			res, err1 := ip.applyOneRule(ctx, r, root)
			if err1 != nil {
				return ex.Wrapf(err1, "applying rule %s", r.GetName())
			}
			hasFuncRule = hasFuncRule || res.needsGlobals
			modified = modified || res.modified
		}
		// Leave a file no rule changed out of the compile command. A glob
		// target attaches call and literal rules to every file of every
		// package it matches, and rewriting those would needlessly reprint
		// them, including cgo and standard library files.
		if !modified {
			continue
		}
		// Since trampoline-jump-if is performance-critical, perform AST level
		// optimization for them before writing to file
		if err = ip.optimizeTJumps(); err != nil {
			return ex.Wrapf(err, "optimizing trampoline jumps for %s", file)
		}
		// Once all func rules targeting this file are applied, write instrumented
		// AST to new file and replace the original file in the compile command
		if err = ip.writeInstrumented(root, file); err != nil {
			return ex.Wrapf(err, "writing instrumented file %s", file)
		}
	}

	// Write globals file if any function is instrumented because injected code
	// always requires some global variables and auxiliary declarations
	if hasFuncRule {
		return ip.writeGlobals(rset.PackageName)
	}
	return nil
}
