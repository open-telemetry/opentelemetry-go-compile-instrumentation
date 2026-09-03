// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"sort"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"go.yaml.in/yaml/v3"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

const LegacyVersion = "v1.0.0"

// Entry is a named rule file entry that can be decoded by its consumer.
type Entry struct {
	Name string
	Node yaml.Node
}

// File is a parsed rule file with entries sorted by name.
type File struct {
	MinimumVersion string
	Legacy         bool
	Entries        []Entry
}

func ParseFile(data []byte) (File, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return File{}, err
	}
	if len(root.Content) == 0 {
		return File{MinimumVersion: LegacyVersion, Legacy: true}, nil
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return File{}, ex.New("rule file root must be a mapping")
	}

	result := File{MinimumVersion: LegacyVersion, Legacy: true}
	seen := make(map[string]struct{})
	for i := 0; i < len(doc.Content); i += 2 {
		key := doc.Content[i]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return File{}, ex.New("rule file mapping keys must be strings")
		}
		name := key.Value
		if _, ok := seen[name]; ok {
			return File{}, ex.Newf("mapping key %q already defined", name)
		}
		seen[name] = struct{}{}

		value := doc.Content[i+1]
		if name != "version" {
			result.Entries = append(result.Entries, Entry{Name: name, Node: *value})
			continue
		}

		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
			return File{}, ex.New("minimum otelc version must be a string")
		}
		if !semver.IsValid(value.Value) || module.IsPseudoVersion(value.Value) {
			return File{}, ex.Newf("minimum otelc version %q is not a valid release version", value.Value)
		}
		result.MinimumVersion = value.Value
		result.Legacy = false
	}

	sort.Slice(result.Entries, func(i, j int) bool {
		return result.Entries[i].Name < result.Entries[j].Name
	})
	return result, nil
}

// Rules constructs the executable instrumentation rules in this file.
func (f File) Rules() ([]InstRule, error) {
	rules := make([]InstRule, 0)
	for _, entry := range f.Entries {
		var fields map[string]any
		if err := entry.Node.Decode(&fields); err != nil {
			return nil, ex.Wrapf(err, "parsing rule %q", entry.Name)
		}
		flatRules, err := Normalize(fields)
		if err != nil {
			return nil, err
		}
		for _, flatFields := range flatRules {
			raw, marshalErr := yaml.Marshal(flatFields)
			if marshalErr != nil {
				return nil, ex.Wrap(marshalErr)
			}
			r, ruleErr := newRule(raw, entry.Name, flatFields)
			if ruleErr != nil {
				return nil, ruleErr
			}
			if strings.TrimSpace(r.GetTarget()) == "" {
				return nil, ex.Newf("rule %q has an empty target; target is required", entry.Name)
			}
			if validateErr := ValidateTarget(r.GetTarget()); validateErr != nil {
				return nil, ex.Wrapf(validateErr, "rule %q", entry.Name)
			}
			if validateErr := util.ValidateVersionRange(r.GetVersion()); validateErr != nil {
				return nil, ex.Wrapf(validateErr, "rule %q", entry.Name)
			}
			rules = append(rules, r)
		}
	}
	return rules, nil
}

func newRule(raw []byte, name string, fields map[string]any) (InstRule, error) {
	switch {
	case fields[selStruct] != nil:
		return NewInstStructRule(raw, name)
	case fields[whereFile] != nil:
		return NewInstFileRule(raw, name)
	case fields[selDirective] != nil:
		return NewInstDirectiveRule(raw, name)
	case fields[rawField] != nil:
		return NewInstRawRule(raw, name)
	case fields[selFunc] != nil:
		return NewInstFuncRule(raw, name)
	case fields[selFunctionCall] != nil:
		return NewInstCallRule(raw, name)
	case fields[selStructLiteral] != nil:
		return NewInstLitRule(raw, name)
	case fields[selIdentifier] != nil:
		return NewInstDeclRule(raw, name)
	default:
		return nil, ex.Newf("rule %q has no recognised selector", name)
	}
}

func CheckVersion(current, required string) error {
	if !semver.IsValid(current) || current == "v0.0.0" || module.IsPseudoVersion(current) {
		return nil
	}
	if semver.Compare(current, required) < 0 {
		return ex.Newf("requires otelc >= %s, current version is %s", required, current)
	}
	return nil
}
