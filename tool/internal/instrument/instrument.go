// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"path/filepath"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
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

// findActualFile finds the actual file in compile arguments that matches the rule file.
// This handles cases where file paths differ between setup and build phases
// (e.g., different module versions in the cache).
func (ip *InstrumentPhase) findActualFile(ruleFile string) (string, error) {
	ruleBasename := filepath.Base(ruleFile)

	// First try exact match
	for _, arg := range ip.compileArgs {
		if !util.IsGoFile(arg) {
			continue
		}
		abs, err := filepath.Abs(arg)
		if err != nil {
			continue
		}
		if abs == ruleFile {
			return abs, nil
		}
	}

	// Fallback to basename match if exact match fails
	for _, arg := range ip.compileArgs {
		if !util.IsGoFile(arg) {
			continue
		}
		abs, err := filepath.Abs(arg)
		if err != nil {
			continue
		}
		if filepath.Base(abs) == ruleBasename {
			ip.Debug("File path mismatch, using basename match",
				"rule_file", ruleFile,
				"actual_file", abs)
			return abs, nil
		}
	}

	return "", ex.Newf("cannot find file %s (basename: %s) in compile args %v",
		ruleFile, ruleBasename, ip.compileArgs)
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
		// Find the actual file in compile args that matches this rule file
		// This handles cases where paths differ between setup and build phases
		actualFile, err := ip.findActualFile(file)
		if err != nil {
			return err
		}

		// Group rules by file, then parse the target file once
		root, err := ip.parseFile(actualFile)
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
		err = ip.writeInstrumented(root, actualFile)
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
