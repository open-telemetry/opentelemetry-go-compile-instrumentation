// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package configuration parses and validates declarative configuration metadata
// consumed by otelc.
package configuration

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.yaml.in/yaml/v3"
)

const (
	yamlMappingPairSize = 2
	yamlStringTag       = "!!str"
	booleanType         = "boolean"
	integerType         = "int"
	stringType          = "string"
	listType            = "list"
	mapType             = "map"
	structuredListType  = "structured_list"
)

// Schema is the structured-list object-schema subset supported by otelc.
type Schema struct {
	// Type is the schema root type; the supported value is object.
	Type string `yaml:"type"`
	// Required lists property keys that must be supplied.
	Required []string `yaml:"required,omitempty"`
	// Properties defines the allowed fields and their types.
	Properties map[string]Property `yaml:"properties"`
}

// Property describes a property in a structured-list schema. Default and
// Example retain the native YAML value type.
type Property struct {
	// Type is the property's declarative value type.
	Type string `yaml:"type"`
	// Description explains the property.
	Description string `yaml:"description,omitempty"`
	// Default is an optional native-YAML default value.
	Default any `yaml:"default,omitempty"`
	// Example is an optional native-YAML example value.
	Example    any `yaml:"example,omitempty"`
	hasDefault bool
	hasExample bool
}

// Entry describes one Go instrumentation configuration option. In source
// metadata it has either a Name (an inline declarative path) or a Ref.
type Entry struct {
	// Name is the OpenTelemetry configuration path relative to instrumentation/development.
	Name string `yaml:"name,omitempty"`
	// Ref identifies a catalog definition; resolved entries retain its provenance.
	Ref string `yaml:"ref,omitempty"`
	// Description explains the option's behavior.
	Description string `yaml:"description,omitempty"`
	// Type is boolean, string, int, map, list, or structured_list.
	Type string `yaml:"type,omitempty"`
	// Default is the top-level string default; nil means absent, not empty.
	Default *string `yaml:"default,omitempty"`
	// Examples contains optional string representations of example values.
	Examples []string `yaml:"examples,omitempty"`
	// Schema describes the item object shape for a structured_list.
	Schema *Schema `yaml:"schema,omitempty"`
	// DefaultBehavior is the prose default behavior from the declarative schema.
	DefaultBehavior string `yaml:"default_behavior,omitempty"`
	// Indicates whether this configuration option is deprecated.
	Deprecated bool `yaml:"deprecated,omitempty"`
	// ReplacedBy identifies the canonical configuration path that supersedes this option.
	ReplacedBy *string `yaml:"replaced_by,omitempty"`
	// Source identifies the metadata.yaml entry that produced this definition.
	Source string `yaml:"-"`
	// DefinitionSource identifies the pinned shared definition used by a ref.
	DefinitionSource string `yaml:"-"`
}

// Metadata is the declarative configuration portion of one module's
// metadata.yaml file.
type Metadata struct {
	// Source is the metadata.yaml file path.
	Source string
	// Entries contains inline definitions and catalog references.
	Entries []Entry
}

func fieldSet(names ...string) map[string]struct{} {
	fields := make(map[string]struct{}, len(names))
	for _, name := range names {
		fields[name] = struct{}{}
	}
	return fields
}

// ParseMetadata reads only the configurations field from metadata.yaml. Other
// top-level metadata fields belong to the watcher/catalog model and are ignored
// here. Every configuration must have exactly one of name or ref.
func ParseMetadata(data []byte, source string) (Metadata, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return Metadata{}, ex.Wrapf(err, "parse %s", source)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return Metadata{}, ex.Newf("parse %s: metadata document must be a mapping", source)
	}

	rootFields, err := mappingFields(document.Content[0], source, nil)
	if err != nil {
		return Metadata{}, err
	}
	configurationsNode, ok := rootFields["configurations"]
	if !ok {
		return Metadata{Source: source}, nil
	}
	if configurationsNode.Kind != yaml.SequenceNode {
		return Metadata{}, ex.Newf("%s: configurations must be a sequence", source)
	}

	metadata := Metadata{Source: source}
	for i, node := range configurationsNode.Content {
		entrySource := fmt.Sprintf("%s:configurations[%d]", source, i)
		entry, parseErr := parseEntry(node, entrySource)
		if parseErr != nil {
			return Metadata{}, parseErr
		}
		if validateErr := validateEntry(entry, entrySource); validateErr != nil {
			return Metadata{}, validateErr
		}
		entry.Source = entrySource
		metadata.Entries = append(metadata.Entries, entry)
	}
	return metadata, nil
}

// parseEntry parses a configurations sequence item. Top-level defaults are
// represented as strings in the metadata model; YAML boolean and numeric
// scalars are normalized to their source spelling to match the Go watcher.
func parseEntry(node *yaml.Node, source string) (Entry, error) {
	fields, err := mappingFields(node, source, fieldSet(
		"name",
		"ref",
		"description",
		"type",
		"default",
		"examples",
		"schema",
		"default_behavior",
		"deprecated",
		"replaced_by",
	))
	if err != nil {
		return Entry{}, err
	}
	_, hasName := fields["name"]
	_, hasRef := fields["ref"]
	if hasName == hasRef {
		return Entry{}, ex.Newf("%s: exactly one of name or ref must be set", source)
	}
	if hasRef && len(fields) != 1 {
		return Entry{}, ex.Newf("%s: a ref entry must not include inline configuration fields", source)
	}

	entry := Entry{}
	for _, field := range []struct {
		name        string
		destination *string
	}{
		{name: "name", destination: &entry.Name},
		{name: "ref", destination: &entry.Ref},
		{name: "description", destination: &entry.Description},
		{name: "type", destination: &entry.Type},
		{name: "default_behavior", destination: &entry.DefaultBehavior},
	} {
		value, present, fieldErr := stringField(fields, field.name, source)
		if fieldErr != nil {
			return Entry{}, fieldErr
		}
		if present {
			*field.destination = value
		}
	}

	if defaultNode, ok := fields["default"]; ok {
		value, defaultErr := defaultString(defaultNode, source+".default")
		if defaultErr != nil {
			return Entry{}, defaultErr
		}
		entry.Default = &value
	}

	if examplesNode, ok := fields["examples"]; ok {
		entry.Examples, err = stringSequence(examplesNode, source+".examples")
		if err != nil {
			return Entry{}, err
		}
	}

	if schemaNode, ok := fields["schema"]; ok {
		entry.Schema, err = parseSchema(schemaNode, source+".schema")
		if err != nil {
			return Entry{}, err
		}
	}
	if deprecatedNode, ok := fields["deprecated"]; ok {
		entry.Deprecated, err = booleanValue(deprecatedNode, source+".deprecated")
		if err != nil {
			return Entry{}, err
		}
	}
	if replacedByNode, ok := fields["replaced_by"]; ok {
		value, stringErr := scalarString(replacedByNode, source+".replaced_by")
		if stringErr != nil {
			return Entry{}, stringErr
		}
		entry.ReplacedBy = &value
	}
	selector, value := "name", entry.Name
	if hasRef {
		selector, value = "ref", entry.Ref
	}
	if strings.TrimSpace(value) == "" {
		return Entry{}, ex.Newf("%s: %s must not be empty", source, selector)
	}
	if strings.TrimSpace(value) != value {
		return Entry{}, ex.Newf("%s: %s must not have leading or trailing whitespace", source, selector)
	}
	return entry, nil
}

func validateEntry(entry Entry, source string) error {
	if entry.Name == "" && entry.Ref != "" {
		return nil
	}
	if !isConfigurationPath(entry.Name) {
		return ex.Newf("%s: name must be a canonical general. or go. configuration path", source)
	}
	if _, err := EnvironmentVariableName(entry.Name); err != nil {
		return ex.Wrapf(err, "%s", source)
	}
	if entry.ReplacedBy != nil {
		if !entry.Deprecated {
			return ex.Newf("%s: replaced_by requires deprecated to be true", source)
		}
		if strings.TrimSpace(*entry.ReplacedBy) == "" {
			return ex.Newf("%s: replaced_by must not be empty", source)
		}
		if strings.TrimSpace(*entry.ReplacedBy) != *entry.ReplacedBy {
			return ex.Newf("%s: replaced_by must not have leading or trailing whitespace", source)
		}
		if !isConfigurationPath(*entry.ReplacedBy) {
			return ex.Newf("%s: replaced_by must be a canonical general. or go. configuration path", source)
		}
	}
	if !supportedType(entry.Type) {
		return ex.Newf("%s: unsupported or missing type %q", source, entry.Type)
	}
	if entry.Schema != nil && entry.Type != structuredListType {
		return ex.Newf("%s: schema requires type structured_list", source)
	}
	if entry.Type == structuredListType && entry.Schema == nil {
		return ex.Newf("%s: structured_list requires schema", source)
	}
	if entry.Schema != nil {
		if err := validateSchema(entry.Schema, source+".schema"); err != nil {
			return err
		}
	}
	if entry.Default != nil {
		if err := validateDefault(*entry.Default, entry.Type, entry.Schema, source+".default"); err != nil {
			return err
		}
	}
	return nil
}

func isConfigurationPath(value string) bool {
	if strings.HasPrefix(value, "general.") {
		return len(value) > len("general.") && !strings.HasSuffix(value, "/development")
	}
	return strings.HasPrefix(value, "go.") && len(value) > len("go.")
}

func supportedType(value string) bool {
	switch value {
	case booleanType, stringType, integerType, mapType, listType, structuredListType:
		return true
	default:
		return false
	}
}

func validateDefault(value, typeName string, schema *Schema, source string) error {
	switch typeName {
	case booleanType:
		if _, err := strconv.ParseBool(value); err != nil {
			return ex.Newf("%s: default %q is not a valid boolean", source, value)
		}
	case integerType:
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return ex.Newf("%s: default %q is not a valid integer", source, value)
		}
	case mapType, listType, structuredListType:
		var decoded any
		if err := yaml.Unmarshal([]byte(value), &decoded); err != nil {
			return ex.Wrapf(err, "%s: parse collection default", source)
		}
		if !valueMatchesType(decoded, typeName) {
			return ex.Newf("%s: default must encode a YAML value of type %q", source, typeName)
		}
		if typeName == structuredListType {
			return validateStructuredDefaults(decoded, schema, source)
		}
	}
	return nil
}

func validateStructuredDefaults(value any, schema *Schema, source string) error {
	items, ok := value.([]any)
	if !ok {
		return ex.Newf("%s: structured_list default must be a sequence", source)
	}
	for i, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return ex.Newf("%s[%d]: structured_list item must be a string-keyed mapping", source, i)
		}
		if err := validateStructuredDefault(object, schema, fmt.Sprintf("%s[%d]", source, i)); err != nil {
			return err
		}
	}
	return nil
}

func validateStructuredDefault(item map[string]any, schema *Schema, source string) error {
	for _, key := range schema.Required {
		if _, exists := item[key]; !exists {
			return ex.Newf("%s: required schema property %q is missing", source, key)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(item)) {
		property, exists := schema.Properties[key]
		if !exists {
			return ex.Newf("%s: unknown schema property %q", source, key)
		}
		if !valueMatchesType(item[key], property.Type) {
			return ex.Newf("%s.%s: value does not match type %q", source, key, property.Type)
		}
	}
	return nil
}

func validateSchema(schema *Schema, source string) error {
	if schema.Type != "object" {
		return ex.Newf("%s: type must be object", source)
	}
	if schema.Properties == nil {
		return ex.Newf("%s: properties must be present", source)
	}
	requiredSeen := make(map[string]struct{}, len(schema.Required))
	for _, key := range schema.Required {
		if _, duplicate := requiredSeen[key]; duplicate {
			return ex.Newf("%s.required: duplicate key %q", source, key)
		}
		requiredSeen[key] = struct{}{}
		if _, ok := schema.Properties[key]; !ok {
			return ex.Newf("%s: required key %q is not defined in properties", source, key)
		}
	}
	propertyNames := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		propertyNames = append(propertyNames, name)
	}
	slices.Sort(propertyNames)
	for _, key := range propertyNames {
		property := schema.Properties[key]
		propertySource := source + ".properties." + key
		if !supportedType(property.Type) {
			return ex.Newf("%s: unsupported property type %q", propertySource, property.Type)
		}
		if property.hasDefault && !valueMatchesType(property.Default, property.Type) {
			return ex.Newf("%s.default: value does not match type %q", propertySource, property.Type)
		}
		if property.hasExample && !valueMatchesType(property.Example, property.Type) {
			return ex.Newf("%s.example: value does not match type %q", propertySource, property.Type)
		}
	}
	return nil
}

func valueMatchesType(value any, typeName string) bool {
	switch typeName {
	case booleanType:
		_, ok := value.(bool)
		return ok
	case stringType:
		_, ok := value.(string)
		return ok
	case integerType:
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			return true
		default:
			return false
		}
	case mapType:
		return isMappingValue(value)
	case listType:
		return isSequenceValue(value)
	case structuredListType:
		items, ok := value.([]any)
		if !ok {
			return false
		}
		for _, item := range items {
			if !isMappingValue(item) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func isMappingValue(value any) bool {
	if value == nil {
		return false
	}
	typeOf := reflect.TypeOf(value)
	return typeOf.Kind() == reflect.Map && typeOf.Key().Kind() == reflect.String
}

func isSequenceValue(value any) bool {
	if value == nil {
		return false
	}
	kind := reflect.ValueOf(value).Kind()
	return kind == reflect.Slice || kind == reflect.Array
}

func parseSchema(node *yaml.Node, source string) (*Schema, error) {
	fields, err := mappingFields(node, source, fieldSet("type", "required", "properties"))
	if err != nil {
		return nil, err
	}
	typeValue, present, err := stringField(fields, "type", source)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, ex.Newf("%s: type is required", source)
	}
	propertiesNode, present := fields["properties"]
	if !present {
		return nil, ex.Newf("%s: properties is required", source)
	}
	propertiesFields, err := mappingFields(propertiesNode, source+".properties", nil)
	if err != nil {
		return nil, err
	}
	schema := &Schema{Type: typeValue, Properties: make(map[string]Property, len(propertiesFields))}
	if requiredNode, ok := fields["required"]; ok {
		schema.Required, err = stringSequence(requiredNode, source+".required")
		if err != nil {
			return nil, err
		}
		requiredSeen := make(map[string]struct{}, len(schema.Required))
		for _, name := range schema.Required {
			if _, duplicate := requiredSeen[name]; duplicate {
				return nil, ex.Newf("%s.required: duplicate key %q", source, name)
			}
			requiredSeen[name] = struct{}{}
		}
	}
	propertyNames := make([]string, 0, len(propertiesFields))
	for name := range propertiesFields {
		propertyNames = append(propertyNames, name)
	}
	slices.Sort(propertyNames)
	for _, name := range propertyNames {
		propertyNode := propertiesFields[name]
		if name == "" {
			return nil, ex.Newf("%s.properties: property name must not be empty", source)
		}
		property, propertyErr := parseProperty(propertyNode, source+".properties."+name)
		if propertyErr != nil {
			return nil, propertyErr
		}
		schema.Properties[name] = property
	}
	return schema, nil
}

func parseProperty(node *yaml.Node, source string) (Property, error) {
	fields, err := mappingFields(node, source, fieldSet("type", "description", "default", "example"))
	if err != nil {
		return Property{}, err
	}
	typeValue, present, err := stringField(fields, "type", source)
	if err != nil {
		return Property{}, err
	}
	if !present {
		return Property{}, ex.Newf("%s: type is required", source)
	}
	property := Property{Type: typeValue}
	if descriptionNode, ok := fields["description"]; ok {
		property.Description, err = scalarString(descriptionNode, source+".description")
		if err != nil {
			return Property{}, err
		}
	}
	if defaultNode, ok := fields["default"]; ok {
		property.Default, err = yamlNodeValue(defaultNode, source+".default")
		if err != nil {
			return Property{}, err
		}
		property.hasDefault = true
	}
	if exampleNode, ok := fields["example"]; ok {
		property.Example, err = yamlNodeValue(exampleNode, source+".example")
		if err != nil {
			return Property{}, err
		}
		property.hasExample = true
	}
	return property, nil
}

func mappingFields(node *yaml.Node, source string, allowed map[string]struct{}) (map[string]*yaml.Node, error) {
	if node.Kind != yaml.MappingNode {
		return nil, ex.Newf("%s: expected a mapping", source)
	}
	fields := make(map[string]*yaml.Node, len(node.Content)/yamlMappingPairSize)
	for i := 0; i < len(node.Content); i += yamlMappingPairSize {
		keyNode, valueNode := node.Content[i], node.Content[i+1]
		key, err := scalarString(keyNode, source+" key")
		if err != nil {
			return nil, err
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, ex.Newf("%s: duplicate key %q", source, key)
		}
		if allowed != nil {
			if _, ok := allowed[key]; !ok {
				return nil, ex.Newf("%s: unknown field %q", source, key)
			}
		}
		fields[key] = valueNode
	}
	return fields, nil
}

func stringField(fields map[string]*yaml.Node, name, source string) (string, bool, error) {
	node, ok := fields[name]
	if !ok {
		return "", false, nil
	}
	value, err := scalarString(node, source+"."+name)
	return value, true, err
}

func scalarString(node *yaml.Node, source string) (string, error) {
	if node.Kind != yaml.ScalarNode || node.Tag != yamlStringTag {
		return "", ex.Newf("%s: expected a string", source)
	}
	return node.Value, nil
}

func booleanValue(node *yaml.Node, source string) (bool, error) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
		return false, ex.Newf("%s: expected a boolean", source)
	}
	value, err := strconv.ParseBool(node.Value)
	if err != nil {
		return false, ex.Wrapf(err, "%s: parse boolean", source)
	}
	return value, nil
}

func defaultString(node *yaml.Node, source string) (string, error) {
	if node.Kind != yaml.ScalarNode {
		return "", ex.Newf("%s: default must be a string, boolean, integer, or float scalar", source)
	}
	switch node.Tag {
	case yamlStringTag, "!!bool", "!!int", "!!float":
		return node.Value, nil
	default:
		return "", ex.Newf("%s: default must be a string, boolean, integer, or float scalar", source)
	}
}

func stringSequence(node *yaml.Node, source string) ([]string, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, ex.Newf("%s: expected a sequence of strings", source)
	}
	values := make([]string, 0, len(node.Content))
	for i, item := range node.Content {
		value, err := scalarString(item, fmt.Sprintf("%s[%d]", source, i))
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func yamlNodeValue(node *yaml.Node, source string) (any, error) {
	var value any
	if err := node.Decode(&value); err != nil {
		return nil, ex.Wrapf(err, "%s: decode YAML value", source)
	}
	return value, nil
}
