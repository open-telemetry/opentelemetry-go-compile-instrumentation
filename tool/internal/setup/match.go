// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bytes"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/data"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"gopkg.in/yaml.v3"
)

func parseEmbeddedRule(path string) ([]*rule.InstFuncRule, error) {
	yamlFile, err := data.ReadEmbedFile(path)
	if err != nil {
		return nil, err
	}
	rules := make(map[string]*rule.InstFuncRule)
	err = yaml.NewDecoder(bytes.NewReader(yamlFile)).Decode(&rules)
	if err != nil {
		return nil, ex.Errorf(err, "failed to decode yaml file")
	}
	arr := make([]*rule.InstFuncRule, 0)
	for name, r := range rules {
		r.Name = name
		arr = append(arr, r)
	}
	return arr, nil
}

func materalizeRules(availables []string) ([]*rule.InstFuncRule, error) {
	parsedRules := []*rule.InstFuncRule{}
	for _, available := range availables {
		rs, err := parseEmbeddedRule(available)
		if err != nil {
			return nil, err
		}
		parsedRules = append(parsedRules, rs...)
	}
	return parsedRules, nil
}

func (sp *SetupPhase) matchedDeps(deps []*Dependency) ([]*rule.InstFuncRule, error) {
	availables, err := data.ListEmbedFiles()
	if err != nil {
		return nil, err
	}
	sp.Info("Available rules", "rules", availables)

	// Construct the set of default rules by parsing embedded data
	rules, err := materalizeRules(availables)
	if err != nil {
		return nil, err
	}

	// Match the default rules with the found dependencies
	matched := make([]*rule.InstFuncRule, 0)
	for _, dep := range deps {
		for _, rule := range rules {
			targetImportPath := rule.GetFuncImportPath()
			targetFunction := rule.GetFuncName()

			// Same import path?
			if targetImportPath != dep.ImportPath {
				continue
			}
			// Iterate over all the source files of the given import path
			// and check if the function is the one we want to instrument
			for _, file := range dep.Sources {
				root, perr := ast.ParseFileFast(file)
				if perr != nil {
					return nil, perr
				}
				funcDecl, perr := ast.FindFuncDecl(root, targetFunction)
				if perr != nil {
					return nil, perr
				}
				if len(funcDecl) > 0 {
					// Okay, this function is the one we want to instrument
					// record the name of the rule that matches this function
					matched = append(matched, rule)
				}
			}
		}
	}
	sp.Info("Matched rules", "matched", matched)
	return matched, nil
}
