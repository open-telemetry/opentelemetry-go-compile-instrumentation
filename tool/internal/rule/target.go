// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"encoding/json"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"go.opentelemetry.io/otelc/tool/ex"
)

// targetKeyNot is the key of a target list entry that excludes packages.
const targetKeyNot = "not"

// Target selects the packages a rule applies to. It is written as a single
// pattern, or as a list of patterns where an entry of the form {not: pattern}
// excludes the packages that pattern matches. Each pattern is an import path,
// a glob, $root or main.
//
// A package is selected when it matches at least one included pattern and no
// excluded one.
type Target struct {
	Include []string
	Exclude []string
}

// NewTarget returns a target that includes each of patterns.
func NewTarget(patterns ...string) Target {
	return Target{Include: patterns}
}

// IsZero reports whether t has no pattern at all.
func (t *Target) IsZero() bool {
	return len(t.Include) == 0 && len(t.Exclude) == 0
}

// String renders t for logs and error messages.
func (t *Target) String() string {
	if t.singular() {
		return t.first()
	}
	parts := make([]string, 0, len(t.Include)+len(t.Exclude))
	parts = append(parts, t.Include...)
	for _, p := range t.Exclude {
		parts = append(parts, targetKeyNot+" "+p)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// Exact returns the import path when t selects a single package by name, so
// matching can look the rule up by key instead of testing patterns.
func (t *Target) Exact() (string, bool) {
	if len(t.Include) != 1 || len(t.Exclude) != 0 {
		return "", false
	}
	p := t.Include[0]
	if IsRootTarget(p) || IsGlobTarget(p) {
		return "", false
	}
	return p, true
}

// IncludesRoot reports whether t includes $root.
func (t *Target) IncludesRoot() bool {
	return slices.Contains(t.Include, TargetRoot)
}

// UsesRoot reports whether any pattern of t, included or excluded, is $root.
func (t *Target) UsesRoot() bool {
	return t.IncludesRoot() || slices.Contains(t.Exclude, TargetRoot)
}

// Validate rejects a target that selects no package or holds a malformed
// pattern. Excluded patterns only remove packages, so a target needs at least
// one included pattern to select anything.
func (t *Target) Validate() error {
	if len(t.Include) == 0 {
		if len(t.Exclude) > 0 {
			return ex.Newf("target %s only has %q entries and selects no package", t, targetKeyNot)
		}
		return ex.New("target is required")
	}
	for _, p := range slices.Concat(t.Include, t.Exclude) {
		if strings.TrimSpace(p) == "" {
			return ex.New("target has an empty pattern")
		}
		if err := ValidateTarget(p); err != nil {
			return err
		}
	}
	return nil
}

// Matches reports whether t selects the package importPath. roots are the
// root module paths that $root stands for; with none, $root matches nothing.
func (t *Target) Matches(importPath string, roots []string) bool {
	return matchesAnyPattern(t.Include, importPath, roots) && !matchesAnyPattern(t.Exclude, importPath, roots)
}

func matchesAnyPattern(patterns []string, importPath string, roots []string) bool {
	for _, p := range patterns {
		if matchPattern(p, importPath, roots) {
			return true
		}
	}
	return false
}

func matchPattern(pattern, importPath string, roots []string) bool {
	switch {
	case IsRootTarget(pattern):
		for _, root := range roots {
			if MatchGlobTarget(root+"/**", importPath) {
				return true
			}
		}
		return false
	case IsGlobTarget(pattern):
		return MatchGlobTarget(pattern, importPath)
	default:
		return pattern == importPath
	}
}

// entries returns t in its list form: included patterns as strings, excluded
// ones as {not: pattern}.
func (t *Target) entries() []any {
	out := make([]any, 0, len(t.Include)+len(t.Exclude))
	for _, p := range t.Include {
		out = append(out, p)
	}
	for _, p := range t.Exclude {
		out = append(out, map[string]string{targetKeyNot: p})
	}
	return out
}

// singular reports whether t is written as a single pattern.
func (t *Target) singular() bool {
	return len(t.Include) <= 1 && len(t.Exclude) == 0
}

func (t *Target) first() string {
	if len(t.Include) == 0 {
		return ""
	}
	return t.Include[0]
}

// UnmarshalYAML accepts a single pattern or a list of patterns and
// {not: pattern} entries.
func (t *Target) UnmarshalYAML(node *yaml.Node) error {
	*t = Target{}
	switch node.Kind {
	case yaml.ScalarNode:
		// A blank value leaves the target unset, which rule loading reports as
		// a missing target.
		if node.Tag != "!!null" && strings.TrimSpace(node.Value) != "" {
			t.Include = []string{node.Value}
		}
		return nil
	case yaml.SequenceNode:
		for _, item := range node.Content {
			switch {
			case item.Kind == yaml.ScalarNode:
				t.Include = append(t.Include, item.Value)
			case item.Kind == yaml.MappingNode && len(item.Content) == 2 &&
				item.Content[0].Value == targetKeyNot && item.Content[1].Kind == yaml.ScalarNode:
				t.Exclude = append(t.Exclude, item.Content[1].Value)
			default:
				return ex.Newf("line %d: a target list entry must be a pattern or {%s: pattern}",
					item.Line, targetKeyNot)
			}
		}
		return nil
	case yaml.DocumentNode, yaml.MappingNode, yaml.AliasNode:
		return ex.Newf("line %d: target must be a pattern or a list of patterns", node.Line)
	default:
		return ex.Newf("line %d: target must be a pattern or a list of patterns", node.Line)
	}
}

// MarshalYAML writes t back in the shape UnmarshalYAML reads.
func (t Target) MarshalYAML() (any, error) {
	if t.singular() {
		return t.first(), nil
	}
	return t.entries(), nil
}

// UnmarshalJSON accepts the same shapes as UnmarshalYAML.
func (t *Target) UnmarshalJSON(data []byte) error {
	*t = Target{}
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		if strings.TrimSpace(single) != "" {
			t.Include = []string{single}
		}
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return ex.Wrapf(err, "target must be a pattern or a list of patterns")
	}
	for _, item := range items {
		var pattern string
		if err := json.Unmarshal(item, &pattern); err == nil {
			t.Include = append(t.Include, pattern)
			continue
		}
		var negated map[string]string
		if err := json.Unmarshal(item, &negated); err != nil || len(negated) != 1 || negated[targetKeyNot] == "" {
			return ex.Newf("a target list entry must be a pattern or {%q: pattern}", targetKeyNot)
		}
		t.Exclude = append(t.Exclude, negated[targetKeyNot])
	}
	return nil
}

// MarshalJSON writes t back in the shape UnmarshalJSON reads.
func (t Target) MarshalJSON() ([]byte, error) {
	if t.singular() {
		return json.Marshal(t.first())
	}
	return json.Marshal(t.entries())
}
