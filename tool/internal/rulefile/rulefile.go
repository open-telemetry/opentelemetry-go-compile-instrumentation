// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rulefile

import (
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"

	"go.opentelemetry.io/otelc/tool/ex"
)

const LegacyVersion = "v1.0.0"

type Document struct {
	MinimumVersion string
	Legacy         bool
	Rules          map[string]yaml.Node
}

func Parse(data []byte) (Document, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return Document{}, err
	}
	if len(root.Content) == 0 {
		return Document{MinimumVersion: LegacyVersion, Legacy: true, Rules: make(map[string]yaml.Node)}, nil
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return Document{}, ex.New("rule file root must be a mapping")
	}

	result := Document{MinimumVersion: LegacyVersion, Legacy: true, Rules: make(map[string]yaml.Node)}
	for i := 0; i < len(doc.Content); i += 2 {
		name := doc.Content[i].Value
		value := doc.Content[i+1]
		if name != "version" {
			result.Rules[name] = *value
			continue
		}

		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
			return Document{}, ex.New("minimum otelc version must be a string")
		}
		if !semver.IsValid(value.Value) || module.IsPseudoVersion(value.Value) {
			return Document{}, ex.Newf("minimum otelc version %q is not a valid release version", value.Value)
		}
		result.MinimumVersion = value.Value
		result.Legacy = false
	}

	return result, nil
}

func CheckVersion(current, required string) error {
	if current == "" || current == "(devel)" || current == "v0.0.0" || module.IsPseudoVersion(current) {
		return nil
	}
	if semver.Compare(current, required) < 0 {
		return ex.Newf("requires otelc >= %s, current version is %s", required, current)
	}
	return nil
}
