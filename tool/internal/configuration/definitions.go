// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	_ "embed"
	"fmt"
	"slices"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.yaml.in/yaml/v3"
)

const (
	// SharedDefinitionsCommit identifies the exact upstream Java source snapshot
	// embedded in otelc. Update it only when intentionally refreshing the copy.
	SharedDefinitionsCommit = "7e2db50673c71761c609275552958a3d5c79ca78"
	sharedDefinitionsPath   = "instrumentation-docs/src/main/resources/shared-config-definitions.yaml"
	// SharedDefinitionsURL links to the source file at the pinned commit.
	SharedDefinitionsURL = "https://github.com/open-telemetry/opentelemetry-java-instrumentation/blob/" + SharedDefinitionsCommit + "/" + sharedDefinitionsPath
)

//go:embed shared-config-definitions.yaml
var embeddedSharedDefinitions []byte

// SharedDefinitions loads the checked-in Java definitions without network
// access. Only instrumentation-level configurations are referenceable by
// module metadata; global_configurations are intentionally excluded.
func SharedDefinitions() (map[string]Entry, error) {
	return ParseSharedDefinitions(embeddedSharedDefinitions)
}

// ParseSharedDefinitions parses the instrumentation-level definitions from a
// copy of Java's shared-config-definitions.yaml.
func ParseSharedDefinitions(data []byte) (map[string]Entry, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, ex.Wrapf(err, "parse shared definitions from %s", SharedDefinitionsURL)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, ex.Newf("shared definitions from %s must be a mapping", SharedDefinitionsURL)
	}
	root, err := mappingFields(document.Content[0], "shared definitions", nil)
	if err != nil {
		return nil, err
	}
	configurationsNode, ok := root["configurations"]
	if !ok {
		return nil, ex.Newf("shared definitions from %s have no configurations mapping", SharedDefinitionsURL)
	}
	definitions, err := mappingFields(configurationsNode, "shared definitions.configurations", nil)
	if err != nil {
		return nil, err
	}
	result := make(map[string]Entry, len(definitions))
	ids := make([]string, 0, len(definitions))
	for id := range definitions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		node := definitions[id]
		source := fmt.Sprintf("shared definition %q", id)
		entry, declarative, parseErr := parseEntry(node, source, true)
		if parseErr != nil {
			return nil, parseErr
		}
		if !declarative || entry.Ref != "" || entry.DeclarativeName == "" {
			return nil, ex.Newf("%s: shared definition must contain declarative_name and inline fields", source)
		}
		if validateErr := validateEntry(entry, source); validateErr != nil {
			return nil, validateErr
		}
		entry.Source = source
		result[id] = entry
	}
	return result, nil
}
