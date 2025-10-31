// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

func groupRules(rset *rule.InstRuleSet) map[string][]rule.InstRule {
	file2rules := make(map[string][]rule.InstRule)
	for file, rules := range rset.FuncRules {
		for _, rule := range rules {
			file2rules[file] = append(file2rules[file], rule)
		}
	}
	for file, rules := range rset.StructRules {
		for _, rule := range rules {
			file2rules[file] = append(file2rules[file], rule)
		}
	}
	for file, rules := range rset.RawRules {
		for _, rule := range rules {
			file2rules[file] = append(file2rules[file], rule)
		}
	}
	return file2rules
}

func (ip *InstrumentPhase) instrument(rset *rule.InstRuleSet) error {
	hasFuncRule := false
	// Apply file rules first because they can introduce new files that used
	// by other rules such as raw rules
	for _, rule := range rset.FileRules {
		err := ip.applyFileRule(rule, rset.PackageName)
		if err != nil {
			return err
		}
	}
	for file, rules := range groupRules(rset) {
		// Group rules by file, then parse the target file once
		root, err := ip.parseFile(file)
		if err != nil {
			return err
		}

		// Apply the rules to the target file
		for _, r := range rules {
			switch rt := r.(type) {
			case *rule.InstFuncRule:
				err1 := ip.applyFuncRule(rt, root)
				if err1 != nil {
					return err1
				}
				hasFuncRule = true
			case *rule.InstStructRule:
				err1 := ip.applyStructRule(rt, root)
				if err1 != nil {
					return err1
				}
			case *rule.InstRawRule:
				err1 := ip.applyRawRule(rt, root)
				if err1 != nil {
					return err1
				}
				hasFuncRule = true
			default:
				util.ShouldNotReachHere()
			}
		}
		// Since trampoline-jump-if is performance-critical, perform AST level
		// optimization for them before writing to file
		err = ip.optimizeTJumps()
		if err != nil {
			return err
		}
		// Once all func rules targeting this file are applied, write instrumented
		// AST to new file and replace the original file in the compile command
		err = ip.writeInstrumented(root, file)
		if err != nil {
			return err
		}
	}

	// Write globals file if any function is instrumented because injected code
	// always requires some global variables and auxiliary declarations
	if hasFuncRule {
		return ip.writeGlobals(rset.PackageName)
	}
	return nil
}
