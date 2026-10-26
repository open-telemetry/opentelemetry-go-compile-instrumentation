// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
)

const envPrefix = "OTEL_INSTRUMENTATION_"

// ResolveMetadata resolves refs in the supplied selected-module metadata,
// deduplicates equivalent definitions, rejects conflicting definitions for the
// same declarative path, and checks derived environment-variable collisions.
func ResolveMetadata(metadataFiles []Metadata, definitions map[string]Entry) ([]Entry, error) {
	byPath := make(map[string]Entry)
	for _, metadata := range metadataFiles {
		for _, entry := range metadata.Entries {
			resolved := entry
			if entry.Ref != "" {
				definition, ok := definitions[entry.Ref]
				if !ok {
					return nil, ex.Newf("%s: unknown configuration ref %q", entry.Source, entry.Ref)
				}
				resolved = cloneEntry(definition)
				resolved.Ref = entry.Ref
				resolved.Source = entry.Source
				resolved.DefinitionSource = definition.Source
			}

			if previous, ok := byPath[resolved.DeclarativeName]; ok {
				if !equivalent(previous, resolved) {
					return nil, ex.Newf(
						"conflicting configuration definitions for declarative_name %q: %s and %s",
						resolved.DeclarativeName,
						entryOrigin(previous),
						entryOrigin(resolved),
					)
				}
				continue
			}
			byPath[resolved.DeclarativeName] = resolved
		}
	}

	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	resolved := make([]Entry, 0, len(paths))
	variables := make(map[string]Entry, len(paths))
	for _, path := range paths {
		entry := byPath[path]
		variable, err := EnvironmentVariableName(path)
		if err != nil {
			return nil, ex.Wrapf(err, "%s", entry.Source)
		}
		if previous, ok := variables[variable]; ok && previous.DeclarativeName != path {
			return nil, ex.Newf(
				"environment variable %s collides between declarative paths %q (%s) and %q (%s)",
				variable,
				previous.DeclarativeName,
				entryOrigin(previous),
				path,
				entryOrigin(entry),
			)
		}
		variables[variable] = entry
		resolved = append(resolved, entry)
	}
	return resolved, nil
}

func entryOrigin(entry Entry) string {
	if entry.Ref != "" && entry.DefinitionSource != "" {
		return fmt.Sprintf("%s (ref %q from %s)", entry.Source, entry.Ref, entry.DefinitionSource)
	}
	return entry.Source
}

// EnvironmentVariableName returns the canonical environment variable for a
// fully resolved declarative path. Java development stability is encoded in
// the terminal /development suffix and is omitted from the variable name.
func EnvironmentVariableName(declarativeName string) (string, error) {
	if declarativeName == "" || strings.TrimSpace(declarativeName) != declarativeName {
		return "", ex.New("declarative_name must be non-empty and have no surrounding whitespace")
	}
	name := strings.TrimPrefix(declarativeName, "general.")
	name = strings.TrimSuffix(name, "/development")
	if name == "" {
		return "", ex.Newf("declarative_name %q has no environment-variable path", declarativeName)
	}
	name = strings.NewReplacer(".", "_", "-", "_", "/", "_").Replace(name)
	name = strings.ToUpper(name)
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
		default:
			return "", ex.Newf("declarative_name %q contains unsupported character %q", declarativeName, r)
		}
	}
	return envPrefix + name, nil
}

func cloneEntry(entry Entry) Entry {
	clone := entry
	if entry.Default != nil {
		value := *entry.Default
		clone.Default = &value
	}
	clone.Examples = slices.Clone(entry.Examples)
	if entry.DeclarativeSchema != nil {
		schema := *entry.DeclarativeSchema
		schema.Required = slices.Clone(entry.DeclarativeSchema.Required)
		schema.Properties = make(map[string]Property, len(entry.DeclarativeSchema.Properties))
		maps.Copy(schema.Properties, entry.DeclarativeSchema.Properties)
		clone.DeclarativeSchema = &schema
	}
	return clone
}

func equivalent(left, right Entry) bool {
	return left.Name == right.Name &&
		left.DeclarativeName == right.DeclarativeName &&
		left.Description == right.Description &&
		left.Type == right.Type &&
		reflect.DeepEqual(left.Default, right.Default) &&
		reflect.DeepEqual(left.Examples, right.Examples) &&
		left.DeclarativeType == right.DeclarativeType &&
		left.DefaultBehavior == right.DefaultBehavior &&
		reflect.DeepEqual(left.DeclarativeSchema, right.DeclarativeSchema)
}
