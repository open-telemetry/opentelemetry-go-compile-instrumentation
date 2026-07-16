// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"

	"gopkg.in/yaml.v3"

	"go.opentelemetry.io/otelc/tool/ex"
)

// ParseRules parses a rule YAML document into instrumentation rules. Entries are
// keyed by rule name; a do clause with N modifiers expands into N rules that
// share the entry's target/version/where. The rule type of each is selected by
// the do modifier name, never inferred from field presence.
func ParseRules(content []byte) ([]InstRule, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, ex.Wrap(err)
	}
	if len(doc.Content) == 0 {
		return nil, nil // empty document
	}
	root := doc.Content[0]
	if root.Kind == yaml.AliasNode {
		root = root.Alias
	}
	if root.Kind != yaml.MappingNode {
		return nil, ex.Newf("rule document must be a mapping of rule name to definition")
	}

	rules := make([]InstRule, 0, len(root.Content))
	for i := 0; i+1 < len(root.Content); i += 2 {
		name := root.Content[i].Value
		built, err := parseRuleEntry(name, root.Content[i+1])
		if err != nil {
			return nil, err
		}
		rules = append(rules, built...)
	}
	return rules, nil
}

// parseRuleEntry builds every rule produced by a single named entry.
func parseRuleEntry(name string, body *yaml.Node) ([]InstRule, error) {
	if body.Kind == yaml.AliasNode {
		body = body.Alias
	}
	if body.Kind != yaml.MappingNode {
		return nil, ex.Newf("rule %q must be a mapping", name)
	}

	// Decode the package-scope base fields through yaml so duplicate keys and
	// merge keys are handled exactly as the pre-refactor map decode did. where
	// and do are grabbed as raw child nodes afterward: yaml.v3 does not populate
	// a *yaml.Node struct field from an already-parsed parent node, and keeping
	// where as a node lets each rule decode its own selectors without a flat
	// re-marshal round-trip.
	var header struct {
		Name    string            `yaml:"name"`
		Target  string            `yaml:"target"`
		Version string            `yaml:"version"`
		Imports map[string]string `yaml:"imports"`
	}
	if err := body.Decode(&header); err != nil {
		return nil, ex.Wrapf(err, "rule %q", name)
	}

	var whereNode, doNode *yaml.Node
	for i := 0; i+1 < len(body.Content); i += 2 {
		switch body.Content[i].Value {
		case "where":
			whereNode = body.Content[i+1]
		case "do":
			doNode = body.Content[i+1]
		}
	}

	if err := validateWhereKeys(whereNode); err != nil {
		return nil, ex.Wrapf(err, "rule %q", name)
	}

	// target is the sole package selector and is required (docs/rules.md). An
	// empty target would land under exactRules[""] and silently never match, so
	// reject it loudly at load time. A malformed glob is rejected here too so a
	// bad rule fails during parsing rather than silently matching nothing.
	if strings.TrimSpace(header.Target) == "" {
		return nil, ex.Newf("rule %q has an empty target; target is required", name)
	}
	if err := ValidateTarget(header.Target); err != nil {
		return nil, ex.Wrapf(err, "rule %q", name)
	}

	var do DoList
	if doNode != nil {
		if err := doNode.Decode(&do); err != nil {
			return nil, ex.Wrapf(err, "rule %q", name)
		}
	}
	if len(do) == 0 {
		return nil, ex.Newf("rule %q is missing do", name)
	}

	filterWhere, err := whereFilter(whereNode)
	if err != nil {
		return nil, ex.Wrapf(err, "rule %q", name)
	}

	ruleName := name
	if header.Name != "" {
		ruleName = header.Name
	}
	base := InstBaseRule{
		Name:    ruleName,
		Target:  header.Target,
		Version: header.Version,
		Imports: header.Imports,
		Where:   filterWhere,
	}

	rules := make([]InstRule, 0, len(do))
	for i := range do {
		r, buildErr := buildRule(base, whereNode, &do[i])
		if buildErr != nil {
			return nil, buildErr
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// buildRule dispatches on the single modifier set in do and constructs the
// matching rule type from the shared base, the where selectors, and the typed
// modifier payload.
func buildRule(base InstBaseRule, where *yaml.Node, do *DoDef) (InstRule, error) {
	switch {
	case do.InjectHooks != nil:
		return NewInstFuncRule(base, where, do.InjectHooks)
	case do.InjectCode != nil:
		return NewInstRawRule(base, where, do.InjectCode)
	case do.WrapCall != nil:
		return NewInstCallRule(base, where, do.WrapCall)
	case do.AddStructFields != nil:
		return NewInstStructRule(base, where, do.AddStructFields)
	case do.AddFile != nil:
		return NewInstFileRule(base, where, do.AddFile)
	case do.ExpandDirective != nil:
		return NewInstDirectiveRule(base, where, do.ExpandDirective)
	case do.AssignValue != nil:
		return NewInstDeclRule(base, where, do.AssignValue)
	default:
		// Unreachable: DoList.UnmarshalYAML enforces exactly one modifier per
		// entry before this dispatch runs.
		return nil, ex.Newf("do entry names no known modifier")
	}
}
