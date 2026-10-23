// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package configuration parses and validates declarative configuration metadata
// consumed by otelc.
package configuration

import (
	"fmt"
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

// Entry is one declarative configuration definition after parsing. Legacy
// flat-only options are intentionally omitted from the returned metadata.
type Entry struct {
	// Name is the existing flat instrumentation option name when present.
	Name string
	// DeclarativeName is the complete OpenTelemetry configuration path.
	DeclarativeName string
	// Ref is the upstream shared-definition identifier, before resolution.
	Ref string
	// Description explains the option's behavior.
	Description string
	// Type preserves the existing flat/Go option type.
	Type string
	// Default is the top-level string default; nil means absent, not empty.
	Default *string
	// Examples contains optional flat examples from the metadata entry.
	Examples []string
	// DeclarativeType is the Java-compatible declarative type when specified.
	DeclarativeType string
	// DeclarativeSchema describes the structured-list object shape.
	DeclarativeSchema *Schema
	// Source identifies the metadata.yaml entry that produced this definition.
	Source string
	// DefinitionSource identifies the pinned shared definition used by a ref.
	DefinitionSource string
}

// Metadata is the declarative configuration portion of one module's
// metadata.yaml file.
type Metadata struct {
	// Source is the metadata.yaml file path.
	Source string
	// Entries contains declarative entries; legacy flat-only entries are omitted.
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
// here. Entries without declarative_name/ref remain valid legacy options.
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
		entry, isDeclarative, parseErr := parseEntry(node, entrySource, false)
		if parseErr != nil {
			return Metadata{}, parseErr
		}
		if !isDeclarative {
			continue
		}
		if validateErr := validateEntry(entry, entrySource); validateErr != nil {
			return Metadata{}, validateErr
		}
		entry.Source = entrySource
		metadata.Entries = append(metadata.Entries, entry)
	}
	return metadata, nil
}

// parseEntry parses a configurations sequence item. Shared-definition defaults
// may use the YAML scalar representation from Java; metadata.yaml defaults must
// already be strings as required by the otelc metadata contract.
func parseEntry(node *yaml.Node, source string, sharedDefault bool) (Entry, bool, error) {
	fields, err := mappingFields(node, source, fieldSet(
		"name",
		"declarative_name",
		"ref",
		"description",
		"type",
		"default",
		"examples",
		"declarative_type",
		"declarative_schema",
	))
	if err != nil {
		return Entry{}, false, err
	}
	selectors, selectorErr := validateSelectors(fields, source)
	if selectorErr != nil {
		return Entry{}, false, selectorErr
	}
	if !selectors.hasDeclarativeName && !selectors.hasRef {
		return Entry{}, false, nil
	}

	entry := Entry{}
	for _, field := range []struct {
		name        string
		destination *string
	}{
		{name: "name", destination: &entry.Name},
		{name: "declarative_name", destination: &entry.DeclarativeName},
		{name: "ref", destination: &entry.Ref},
		{name: "description", destination: &entry.Description},
		{name: "type", destination: &entry.Type},
		{name: "declarative_type", destination: &entry.DeclarativeType},
	} {
		value, present, fieldErr := stringField(fields, field.name, source)
		if fieldErr != nil {
			return Entry{}, false, fieldErr
		}
		if present {
			*field.destination = value
		}
	}

	if defaultNode, ok := fields["default"]; ok {
		value, defaultErr := defaultString(defaultNode, sharedDefault, source+".default")
		if defaultErr != nil {
			return Entry{}, false, defaultErr
		}
		entry.Default = &value
	}

	if examplesNode, ok := fields["examples"]; ok {
		entry.Examples, err = stringSequence(examplesNode, source+".examples")
		if err != nil {
			return Entry{}, false, err
		}
	}

	if schemaNode, ok := fields["declarative_schema"]; ok {
		entry.DeclarativeSchema, err = parseSchema(schemaNode, source+".declarative_schema")
		if err != nil {
			return Entry{}, false, err
		}
	}

	if selectors.hasDeclarativeName && entry.DeclarativeName == "" {
		return Entry{}, false, ex.Newf("%s: declarative_name must not be empty", source)
	}
	if selectors.hasRef && entry.Ref == "" {
		return Entry{}, false, ex.Newf("%s: ref must not be empty", source)
	}
	if selectors.hasDeclarativeName && strings.TrimSpace(entry.DeclarativeName) != entry.DeclarativeName {
		return Entry{}, false, ex.Newf("%s: declarative_name must not have leading or trailing whitespace", source)
	}
	if selectors.hasRef && strings.TrimSpace(entry.Ref) != entry.Ref {
		return Entry{}, false, ex.Newf("%s: ref must not have leading or trailing whitespace", source)
	}
	if selectors.hasRef && hasInlineFieldKeys(fields) {
		return Entry{}, false, ex.Newf("%s: a ref entry must not include inline configuration fields", source)
	}
	return entry, true, nil
}

type selectors struct {
	hasDeclarativeName bool
	hasRef             bool
}

func validateSelectors(fields map[string]*yaml.Node, source string) (selectors, error) {
	_, hasName := fields["declarative_name"]
	_, hasRef := fields["ref"]
	_, hasDeclarativeType := fields["declarative_type"]
	_, hasDeclarativeSchema := fields["declarative_schema"]
	if hasName && hasRef {
		return selectors{}, ex.Newf("%s: exactly one of declarative_name or ref must be set", source)
	}
	if !hasName && !hasRef && (hasDeclarativeType || hasDeclarativeSchema) {
		return selectors{}, ex.Newf(
			"%s: declarative_type or declarative_schema requires declarative_name or ref",
			source,
		)
	}
	return selectors{hasDeclarativeName: hasName, hasRef: hasRef}, nil
}

func hasInlineFieldKeys(fields map[string]*yaml.Node) bool {
	for _, field := range []string{"name", "description", "type", "default", "examples", "declarative_type", "declarative_schema"} {
		if _, ok := fields[field]; ok {
			return true
		}
	}
	return false
}

func validateEntry(entry Entry, source string) error {
	if entry.DeclarativeName != "" && strings.TrimSpace(entry.DeclarativeName) != entry.DeclarativeName {
		return ex.Newf("%s: declarative_name must not have leading or trailing whitespace", source)
	}
	if entry.Ref != "" && strings.TrimSpace(entry.Ref) != entry.Ref {
		return ex.Newf("%s: ref must not have leading or trailing whitespace", source)
	}
	if entry.DeclarativeType != "" && !supportedType(entry.DeclarativeType) {
		return ex.Newf("%s: unsupported declarative_type %q", source, entry.DeclarativeType)
	}
	if entry.DeclarativeSchema != nil && entry.DeclarativeType != structuredListType {
		return ex.Newf("%s: declarative_schema requires declarative_type structured_list", source)
	}
	if entry.DeclarativeType == structuredListType && entry.DeclarativeSchema == nil {
		return ex.Newf("%s: structured_list requires declarative_schema", source)
	}
	if entry.DeclarativeSchema != nil {
		if err := validateSchema(entry.DeclarativeSchema, source+".declarative_schema"); err != nil {
			return err
		}
	}
	if entry.Default != nil {
		if err := validateDefault(*entry.Default, entry.DeclarativeType, entry.Type, source+".default"); err != nil {
			return err
		}
	}
	return nil
}

func supportedType(value string) bool {
	switch value {
	case booleanType, "string", integerType, "map", "list", structuredListType:
		return true
	default:
		return false
	}
}

func validateDefault(value, declarativeType, flatType, source string) error {
	typeName := declarativeType
	if typeName == "" {
		typeName = flatType
	}
	switch typeName {
	case booleanType, "bool":
		if _, err := strconv.ParseBool(value); err != nil {
			return ex.Newf("%s: default %q is not a valid boolean", source, value)
		}
	case integerType, "integer":
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return ex.Newf("%s: default %q is not a valid integer", source, value)
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
	case "string":
		_, ok := value.(string)
		return ok
	case integerType:
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			return true
		default:
			return false
		}
	case "map":
		return isMappingValue(value)
	case "list":
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

func defaultString(node *yaml.Node, shared bool, source string) (string, error) {
	if node.Kind != yaml.ScalarNode {
		return "", ex.Newf("%s: default must be a scalar string", source)
	}
	if !shared && node.Tag != yamlStringTag {
		return "", ex.Newf("%s: default must be a string; quote scalar values", source)
	}
	switch node.Tag {
	case yamlStringTag, "!!bool", "!!int", "!!float":
		return node.Value, nil
	default:
		return "", ex.Newf("%s: unsupported default scalar type %s", source, node.Tag)
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
