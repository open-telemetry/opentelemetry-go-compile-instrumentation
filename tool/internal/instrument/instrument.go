// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"path/filepath"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

func groupRules(workDir string, rset *rule.InstRuleSet) map[string][]rule.InstRule {
	file2rules := make(map[string][]rule.InstRule)
	addRulesToMap(rset.FuncRules, file2rules, rset.CgoFileMap, workDir)
	addRulesToMap(rset.StructRules, file2rules, rset.CgoFileMap, workDir)
	addRulesToMap(rset.RawRules, file2rules, rset.CgoFileMap, workDir)
	addRulesToMap(rset.CallRules, file2rules, rset.CgoFileMap, workDir)
	addRulesToMap(rset.DirectiveRules, file2rules, rset.CgoFileMap, workDir)
	addRulesToMap(rset.DeclRules, file2rules, rset.CgoFileMap, workDir)
	return file2rules
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

// applyOneRule applies a single rule to the target file and reports whether
// the rule counts as a function rule (i.e. whether a globals file is needed).
func (ip *InstrumentPhase) applyOneRule(ctx context.Context, r rule.InstRule, root *dst.File) (bool, error) {
	switch rt := r.(type) {
	case *rule.InstFuncRule:
		return true, ip.applyFuncRule(ctx, rt, root)
	case *rule.InstStructRule:
		return false, ip.applyStructRule(ctx, rt, root)
	case *rule.InstDeclRule:
		return false, ip.applyDeclRule(ctx, rt, root)
	case *rule.InstRawRule:
		return true, ip.applyRawRule(ctx, rt, root)
	case *rule.InstCallRule:
		return false, ip.applyCallRule(ctx, rt, root)
	case *rule.InstDirectiveRule:
		return true, ip.applyDirectiveRule(ctx, rt, root)
	default:
		util.ShouldNotReachHere()
		return false, nil
	}
}

func (ip *InstrumentPhase) instrument(ctx context.Context, rset *rule.InstRuleSet) error {
	hasFuncRule := false
	// Apply file rules first because they can introduce new files that used
	// by other rules such as raw rules
	for _, rule := range rset.FileRules {
		err := ip.applyFileRule(ctx, rule, rset.PackageName)
		if err != nil {
			return ex.Wrapf(err, "applying file rule %s to package %s", rule.Name, rset.PackageName)
		}
	}
	for file, rules := range groupRules(ip.workDir, rset) {
		// Group rules by file, then parse the target file once
		root, err := ip.parseFile(file)
		if err != nil {
			return ex.Wrapf(err, "parsing file %s", file)
		}

		// Apply the rules to the target file
		for _, r := range rules {
			funcRule, err1 := ip.applyOneRule(ctx, r, root)
			if err1 != nil {
				return ex.Wrapf(err1, "applying rule %s", r.GetName())
			}
			hasFuncRule = hasFuncRule || funcRule
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
