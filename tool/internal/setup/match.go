// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"github.com/dave/dst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/data"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"gopkg.in/yaml.v3"
)

// parseEmbeddedRule parses the embedded yaml rule file to concrete rule instances
//
//nolint:nestif // It has many if statements, but it's straightforward
func parseEmbeddedRule(path string) ([]rule.InstRule, error) {
	yamlFile, err := data.ReadEmbedFile(path)
	if err != nil {
		return nil, err
	}
	var h map[string]map[string]any
	err = yaml.Unmarshal(yamlFile, &h)
	if err != nil {
		return nil, ex.Wrap(err)
	}
	rules := make([]rule.InstRule, 0)
	for name, fields := range h {
		raw, err1 := yaml.Marshal(fields)
		if err1 != nil {
			return nil, ex.Wrap(err1)
		}

		if _, ok := fields["struct"]; ok {
			var r rule.InstStructRule
			err2 := yaml.Unmarshal(raw, &r)
			if err2 != nil {
				return nil, ex.Wrap(err2)
			}
			r.Name = name
			r.Target, ok = fields["target"].(string)
			util.Assert(ok, "target is not a string")
			rules = append(rules, &r)
		} else if _, ok2 := fields["file"]; ok2 {
			var r rule.InstFileRule
			err2 := yaml.Unmarshal(raw, &r)
			if err2 != nil {
				return nil, ex.Wrap(err2)
			}
			r.Name = name
			r.Target, ok = fields["target"].(string)
			util.Assert(ok, "target is not a string")
			rules = append(rules, &r)
		} else if _, ok3 := fields["raw"]; ok3 {
			var r rule.InstRawRule
			err2 := yaml.Unmarshal(raw, &r)
			if err2 != nil {
				return nil, ex.Wrap(err2)
			}
			r.Name = name
			r.Target, ok = fields["target"].(string)
			util.Assert(ok, "target is not a string")
			rules = append(rules, &r)
		} else if _, ok4 := fields["func"]; ok4 {
			var r rule.InstFuncRule
			err2 := yaml.Unmarshal(raw, &r)
			if err2 != nil {
				return nil, ex.Wrap(err2)
			}
			r.Name = name
			r.Target, ok = fields["target"].(string)
			util.Assert(ok, "target is not a string")
			rules = append(rules, &r)
		} else {
			util.ShouldNotReachHere()
		}
	}
	return rules, nil
}

// materalizeRules materializes all available rules from the embedded data
func materalizeRules() ([]rule.InstRule, error) {
	availables, err := data.ListEmbedFiles()
	if err != nil {
		return nil, err
	}

	parsedRules := []rule.InstRule{}
	for _, available := range availables {
		rs, perr := parseEmbeddedRule(available)
		if perr != nil {
			return nil, perr
		}
		parsedRules = append(parsedRules, rs...)
	}
	return parsedRules, nil
}

func runMatch(dependency *Dependency, allRules []rule.InstRule) (*rule.InstRuleSet, error) {
	parsedAst := make(map[string]*dst.File)
	set := rule.NewInstRuleSet(dependency.ImportPath)

	// Not all rules are applicable to the dependency, we can quickly filter out
	// the rules based on the target module path.
	rules := make([]rule.InstRule, 0)
	for _, r := range allRules {
		if r.GetTarget() != dependency.ImportPath {
			continue
		}
		rules = append(rules, r)
		// Do further. If the rule is a file rule, it always applicable
		if fr, ok := r.(*rule.InstFileRule); ok {
			set.AddFileRule(fr)
		}
	}
	for _, source := range dependency.Sources {
		// Fair enough, parse the file content. Since this is a heavy operation,
		// we cache the parsed AST to avoid redundant parsing.
		tree, found := parsedAst[source]
		if !found {
			root, err := ast.ParseFileFast(source) // Match only, no node updates
			if err != nil {
				return nil, err
			}
			parsedAst[source] = root
			set.SetPackageName(root.Name.Name)
			tree = root
		}
		if tree == nil {
			return nil, ex.Newf("failed to parse file %s", source)
		}

		for _, available := range rules {
			// Let's match with the rule precisely
			switch rt := available.(type) {
			case *rule.InstFuncRule:
				funcDecl := ast.FindFuncDecl(tree, rt.Func)
				if len(funcDecl) > 0 {
					set.AddFuncRule(source, rt)
				}
			case *rule.InstStructRule:
				structDecl := ast.FindStructDecl(tree, rt.Struct)
				if structDecl != nil {
					set.AddStructRule(source, rt)
				}
			case *rule.InstRawRule:
				funcDecl := ast.FindFuncDecl(tree, rt.Func)
				if len(funcDecl) > 0 {
					set.AddRawRule(source, rt)
				}
			case *rule.InstFileRule:
				// Skip as it's already processed
			default:
				util.ShouldNotReachHere()
			}
		}
	}
	return set, nil
}

func (sp *SetupPhase) matchDeps(deps []*Dependency) ([]*rule.InstRuleSet, error) {
	// Construct the set of default allRules by parsing embedded data
	allRules, err := materalizeRules()
	if err != nil {
		return nil, err
	}
	sp.Info("Available rules", "rules", allRules)
	if len(allRules) == 0 {
		return nil, nil
	}

	// Match the default rules with the found dependencies
	matched := make([]*rule.InstRuleSet, 0)
	for _, dep := range deps {
		// TODO: Parallelize this
		m, err1 := runMatch(dep, allRules)
		if err1 != nil {
			return nil, err1
		}
		if !m.IsEmpty() {
			matched = append(matched, m)
		}
	}
	sp.Info("Match rule sets", "sets", matched)
	return matched, nil
}
