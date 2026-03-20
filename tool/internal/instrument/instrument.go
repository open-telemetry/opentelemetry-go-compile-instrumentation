// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"path/filepath"

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
	// DirectiveRules are intentionally excluded: they are a pure filter with
	// no advice, so there is no AST transformation to apply. Including them
	// would cause a wasted parse+write cycle for directive-only files.
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

//nolint:gocognit
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
			switch rt := r.(type) {
			case *rule.InstFuncRule:
				err1 := ip.applyFuncRule(ctx, rt, root)
				if err1 != nil {
					return ex.Wrapf(err1, "applying func rule %s to %s", rt.Name, file)
				}
				hasFuncRule = true
			case *rule.InstStructRule:
				err1 := ip.applyStructRule(ctx, rt, root)
				if err1 != nil {
					return ex.Wrapf(err1, "applying struct rule %s to %s", rt.Name, file)
				}
			case *rule.InstRawRule:
				err1 := ip.applyRawRule(ctx, rt, root)
				if err1 != nil {
					return ex.Wrapf(err1, "applying raw rule %s to %s", rt.Name, file)
				}
				hasFuncRule = true
			case *rule.InstCallRule:
				err1 := ip.applyCallRule(ctx, rt, root)
				if err1 != nil {
					return err1
				}
			default:
				util.ShouldNotReachHere()
			}
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
