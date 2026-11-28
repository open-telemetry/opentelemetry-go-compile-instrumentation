// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	otelconfx "go.opentelemetry.io/contrib/otelconf/x"
	"go.opentelemetry.io/otelc/tool/ex"
	"go.yaml.in/yaml/v3"
)

const (
	generalConfigurationSection    = "general"
	goConfigurationSection         = "go"
	minimumConfigurationParts      = 2
	instrumentationDevelopmentPath = "instrumentation/development"
)

// SnapshotVersion is the wire version of the normalized JSON configuration
// embedded in instrumented applications.
const SnapshotVersion = 1

// ApplicationConfiguration is the explicit source document retained until
// setup classifies instrumentation options against matched-module metadata.
type ApplicationConfiguration struct {
	Source          string
	Document        *yaml.Node
	Instrumentation map[string]any
	SourceLocations map[string]SourceLocation
}

// SourceLocation identifies the source location of a supplied application
// configuration value.
type SourceLocation struct {
	File   string
	Line   int
	Column int
}

// ApplicationConfigurationWarning reports an option that does not apply to
// selected instrumentation.
type ApplicationConfigurationWarning struct {
	Path     string
	Location SourceLocation
	Message  string
}

// ValidatedApplicationConfiguration contains values validated for selected
// instrumentation. It is the setup-to-embedding handoff for #1491.
type ValidatedApplicationConfiguration struct {
	Parsed         *otelconfx.OpenTelemetryConfiguration
	Values         map[string]any
	ResolvedValues map[string]any
	Snapshot       []byte
	Identity       []byte
	Warnings       []ApplicationConfigurationWarning
}

// ParseApplicationConfiguration reads one YAML document and parses its SDK
// configuration with the pinned otelconf/x model. The instrumentation subtree
// is retained until setup classifies it against matched-module metadata, so
// malformed options for unselected instrumentation do not fail setup.
func ParseApplicationConfiguration(data []byte, source string) (ApplicationConfiguration, error) {
	root, err := parseSingleYAMLDocument(data, source)
	if err != nil {
		return ApplicationConfiguration{}, err
	}
	if err = rejectDuplicateYAMLKeys(root, source); err != nil {
		return ApplicationConfiguration{}, err
	}
	if !hasYAMLField(root, "file_format") {
		return ApplicationConfiguration{}, ex.Newf("%s: file_format is required", source)
	}
	if err = validateApplicationRootFields(
		root,
		reflect.TypeFor[otelconfx.OpenTelemetryConfiguration](),
		source,
	); err != nil {
		return ApplicationConfiguration{}, err
	}
	application := ApplicationConfiguration{
		Source:          source,
		Document:        root,
		SourceLocations: make(map[string]SourceLocation),
	}
	rootValues, err := yamlNodeMapping(root, source)
	if err != nil {
		return ApplicationConfiguration{}, err
	}
	sdkValues := maps.Clone(rootValues)
	delete(sdkValues, instrumentationDevelopmentPath)
	sdkBytes, err := yaml.Marshal(sdkValues)
	if err != nil {
		return ApplicationConfiguration{}, ex.Wrapf(err, "normalize SDK configuration in %s", source)
	}
	sdkConfig, err := otelconfx.ParseYAML(sdkBytes)
	if err != nil {
		return ApplicationConfiguration{}, ex.Wrapf(err, "parse SDK configuration in %s", source)
	}
	if err = validateFileFormat(sdkConfig.FileFormat); err != nil {
		return ApplicationConfiguration{}, ex.Wrapf(err, "%s", source)
	}
	if applicationNode, ok := rootValues[instrumentationDevelopmentPath]; ok {
		if applicationNode.Kind != yaml.MappingNode {
			return ApplicationConfiguration{}, ex.Newf(
				"%s: %s must be an object", source, instrumentationDevelopmentPath,
			)
		}
		if err = applicationNode.Decode(&application.Instrumentation); err != nil {
			return ApplicationConfiguration{}, ex.Wrapf(err, "decode %s in %s", instrumentationDevelopmentPath, source)
		}
	}
	indexInstrumentationLocations(root, source, application.SourceLocations)
	return application, nil
}

func overlayEnvironmentValues(
	sourceNode *yaml.Node,
	parsed *otelconfx.OpenTelemetryConfiguration,
	source string,
) error {
	encoded, err := yaml.Marshal(parsed)
	if err != nil {
		return ex.Wrapf(err, "normalize environment substitutions in %s", source)
	}
	var parsedDocument yaml.Node
	if err = yaml.Unmarshal(encoded, &parsedDocument); err != nil {
		return ex.Wrapf(err, "decode normalized configuration %s", source)
	}
	if sourceNode.Kind != yaml.MappingNode || len(parsedDocument.Content) != 1 {
		return ex.Newf("%s: application configuration must contain one mapping", source)
	}
	return overlayEnvironmentNode(sourceNode, parsedDocument.Content[0], source, "")
}

func overlayEnvironmentNode(sourceNode, parsedNode *yaml.Node, source, path string) error {
	if sourceNode == nil || parsedNode == nil {
		return nil
	}
	if sourceNode.Kind == yaml.AliasNode {
		return overlayEnvironmentNode(sourceNode.Alias, parsedNode, source, path)
	}
	if sourceNode.Kind == yaml.ScalarNode {
		return overlayEnvironmentScalar(sourceNode, parsedNode, source, path)
	}
	if sourceNode.Kind == yaml.MappingNode && parsedNode.Kind == yaml.MappingNode {
		return overlayEnvironmentMapping(sourceNode, parsedNode, source, path)
	}
	if sourceNode.Kind == yaml.SequenceNode && parsedNode.Kind == yaml.SequenceNode {
		return overlayEnvironmentSequence(sourceNode, parsedNode, source, path)
	}
	return nil
}

func overlayEnvironmentScalar(sourceNode, parsedNode *yaml.Node, source, path string) error {
	if !strings.Contains(sourceNode.Value, "${") {
		return nil
	}
	if parsedNode.Kind != yaml.ScalarNode {
		return ex.Newf("%s: environment substitution at %s did not produce a scalar", source, path)
	}
	sourceNode.Tag = parsedNode.Tag
	sourceNode.Value = parsedNode.Value
	sourceNode.Style = parsedNode.Style
	return nil
}

func overlayEnvironmentMapping(sourceNode, parsedNode *yaml.Node, source, path string) error {
	parsedFields := make(map[string]*yaml.Node, len(parsedNode.Content)/yamlMappingPairSize)
	for i := 0; i+1 < len(parsedNode.Content); i += yamlMappingPairSize {
		parsedFields[parsedNode.Content[i].Value] = parsedNode.Content[i+1]
	}
	for i := 0; i+1 < len(sourceNode.Content); i += yamlMappingPairSize {
		key := sourceNode.Content[i]
		parsedValue, ok := parsedFields[key.Value]
		if !ok {
			continue
		}
		if err := overlayEnvironmentNode(
			sourceNode.Content[i+1],
			parsedValue,
			source,
			joinYAMLPath(path, key.Value),
		); err != nil {
			return err
		}
	}
	return nil
}

func overlayEnvironmentSequence(sourceNode, parsedNode *yaml.Node, source, path string) error {
	for i, value := range sourceNode.Content {
		if i >= len(parsedNode.Content) {
			break
		}
		if err := overlayEnvironmentNode(
			value,
			parsedNode.Content[i],
			source,
			fmt.Sprintf("%s[%d]", path, i),
		); err != nil {
			return err
		}
	}
	return nil
}

func yamlNodeMapping(node *yaml.Node, source string) (map[string]*yaml.Node, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, ex.Newf("%s: application configuration must be a mapping", source)
	}
	fields := make(map[string]*yaml.Node, len(node.Content)/yamlMappingPairSize)
	for i := 0; i+1 < len(node.Content); i += yamlMappingPairSize {
		fields[node.Content[i].Value] = node.Content[i+1]
	}
	return fields, nil
}

// ApplicationOptionValues returns the supplied scalar and collection-valued
// option paths. Nested object values are traversed unless a definition
// identifies that path as an option value.
func (app ApplicationConfiguration) ApplicationOptionValues(definitions map[string]Entry) map[string]any {
	values := make(map[string]any)
	if app.Instrumentation == nil {
		return values
	}
	for _, section := range []string{generalConfigurationSection, goConfigurationSection} {
		value, exists := app.Instrumentation[section]
		if !exists {
			continue
		}
		collectApplicationOptionValues(value, section, definitions, values)
	}
	return values
}

// ValidateApplicationOptions classifies source options after instrumentation
// matching, validates only selected values, and returns warnings for
// unselected configuration. If unresolved options remain while selected
// modules lack metadata, it fails rather than silently dropping them.
func ValidateApplicationOptions(
	application ApplicationConfiguration,
	selectedDefinitions []Entry,
	modulesWithoutMetadata []string,
) (ValidatedApplicationConfiguration, error) {
	selected := make(map[string]Entry, len(selectedDefinitions))
	for _, entry := range selectedDefinitions {
		selected[entry.Name] = entry
	}
	if application.Document == nil {
		if application.Source != "" {
			return ValidatedApplicationConfiguration{}, ex.Newf(
				"%s: application configuration must be a mapping", application.Source,
			)
		}
		resolvedValues, resolveErr := resolveApplicationDefaults(selected, nil)
		if resolveErr != nil {
			return ValidatedApplicationConfiguration{}, resolveErr
		}
		snapshot, snapshotErr := applicationSnapshot(selected, resolvedValues)
		if snapshotErr != nil {
			return ValidatedApplicationConfiguration{}, snapshotErr
		}
		return ValidatedApplicationConfiguration{
			ResolvedValues: resolvedValues,
			Snapshot:       snapshot.Snapshot,
			Identity:       snapshot.Identity,
		}, nil
	}
	catalog, err := CatalogDefinitions()
	if err != nil {
		return ValidatedApplicationConfiguration{}, ex.Wrapf(err, "load pinned configuration catalog")
	}
	allDefinitions := make(map[string]Entry, len(catalog)+len(selected))
	maps.Copy(allDefinitions, catalog)
	maps.Copy(allDefinitions, selected)
	if err = validateApplicationPathSegments(application, allDefinitions); err != nil {
		return ValidatedApplicationConfiguration{}, err
	}
	optionValues := application.ApplicationOptionValues(allDefinitions)
	filteredDevelopment, warnings, err := filterSelectedApplicationOptions(
		application,
		optionValues,
		selected,
		catalog,
		modulesWithoutMetadata,
	)
	if err != nil {
		return ValidatedApplicationConfiguration{}, err
	}
	parsed, normalizedDevelopment, err := parseAndValidateSelectedApplicationConfiguration(
		application,
		filteredDevelopment,
		selected,
	)
	if err != nil {
		return ValidatedApplicationConfiguration{}, err
	}
	values, deprecatedWarnings, err := validateSelectedApplicationValues(application, normalizedDevelopment, selected)
	if err != nil {
		return ValidatedApplicationConfiguration{}, err
	}
	resolvedValues, err := resolveApplicationDefaults(selected, values)
	if err != nil {
		return ValidatedApplicationConfiguration{}, err
	}
	snapshot, err := applicationSnapshot(selected, resolvedValues)
	if err != nil {
		return ValidatedApplicationConfiguration{}, err
	}
	return ValidatedApplicationConfiguration{
		Parsed:         parsed,
		Values:         values,
		ResolvedValues: resolvedValues,
		Snapshot:       snapshot.Snapshot,
		Identity:       snapshot.Identity,
		Warnings:       append(warnings, deprecatedWarnings...),
	}, nil
}

func resolveApplicationDefaults(selected map[string]Entry, values map[string]any) (map[string]any, error) {
	resolved := maps.Clone(values)
	if resolved == nil {
		resolved = make(map[string]any)
	}
	for path, entry := range selected {
		if _, exists := resolved[path]; exists || entry.Default == nil {
			continue
		}
		value, err := applicationDefaultValue(*entry.Default, entry.Type)
		if err != nil {
			return nil, ex.Wrapf(err, "decode default for %s", path)
		}
		resolved[path] = value
	}
	return resolved, nil
}

type applicationSnapshotDocument struct {
	Version int            `json:"version"`
	Values  map[string]any `json:"values"`
}

type applicationSnapshotResult struct {
	Snapshot []byte
	Identity []byte
}

type applicationDefinitionIdentity struct {
	Type       string                     `json:"type"`
	Default    *string                    `json:"default,omitempty"`
	Schema     *applicationSchemaIdentity `json:"schema,omitempty"`
	Validation *ValueConstraints          `json:"validation,omitempty"`
	Deprecated bool                       `json:"deprecated,omitempty"`
	ReplacedBy *string                    `json:"replaced_by,omitempty"`
}

type applicationSchemaIdentity struct {
	Type       string                                 `json:"type"`
	Required   []string                               `json:"required,omitempty"`
	Properties map[string]applicationPropertyIdentity `json:"properties"`
}

type applicationPropertyIdentity struct {
	Type       string `json:"type"`
	Default    any    `json:"default,omitempty"`
	HasDefault bool   `json:"has_default,omitempty"`
}

// applicationSnapshot returns the runtime payload and cache identity from the
// same normalized values. Source paths and descriptive metadata are excluded:
// moving a metadata file or editing documentation does not change behavior.
func applicationSnapshot(
	selected map[string]Entry,
	resolvedValues map[string]any,
) (applicationSnapshotResult, error) {
	var result applicationSnapshotResult
	if len(resolvedValues) > 0 {
		snapshot, err := json.Marshal(applicationSnapshotDocument{
			Version: SnapshotVersion,
			Values:  resolvedValues,
		})
		if err != nil {
			return applicationSnapshotResult{}, ex.Wrapf(err, "encode resolved application configuration")
		}
		result.Snapshot = snapshot
	}
	if len(selected) == 0 && len(resolvedValues) == 0 {
		return result, nil
	}
	identityDefinitions := make(map[string]applicationDefinitionIdentity, len(selected))
	for name, entry := range selected {
		identityDefinitions[name] = applicationDefinitionIdentity{
			Type:       entry.Type,
			Default:    entry.Default,
			Schema:     applicationSchemaForIdentity(entry.Schema),
			Validation: entry.Validation,
			Deprecated: entry.Deprecated,
			ReplacedBy: entry.ReplacedBy,
		}
	}
	identity, err := json.Marshal(struct {
		Version     int                                      `json:"version"`
		Definitions map[string]applicationDefinitionIdentity `json:"definitions"`
		Values      map[string]any                           `json:"values"`
	}{
		Version:     SnapshotVersion,
		Definitions: identityDefinitions,
		Values:      resolvedValues,
	})
	if err != nil {
		return applicationSnapshotResult{}, ex.Wrapf(err, "encode application configuration identity")
	}
	result.Identity = identity
	return result, nil
}

func applicationSchemaForIdentity(schema *Schema) *applicationSchemaIdentity {
	if schema == nil {
		return nil
	}
	required := slices.Clone(schema.Required)
	slices.Sort(required)
	properties := make(map[string]applicationPropertyIdentity, len(schema.Properties))
	for name, property := range schema.Properties {
		properties[name] = applicationPropertyIdentity{
			Type:       property.Type,
			Default:    property.Default,
			HasDefault: property.hasDefault,
		}
	}
	return &applicationSchemaIdentity{
		Type:       schema.Type,
		Required:   required,
		Properties: properties,
	}
}

func applicationDefaultValue(defaultValue, typeName string) (any, error) {
	switch typeName {
	case stringType:
		return defaultValue, nil
	case booleanType:
		return strconv.ParseBool(defaultValue)
	case integerType:
		value, err := strconv.ParseInt(defaultValue, 10, 64)
		if err != nil {
			return nil, err
		}
		if value >= math.MinInt && value <= math.MaxInt {
			return int(value), nil
		}
		return value, nil
	case mapType, listType, structuredListType:
		var value any
		if err := yaml.Unmarshal([]byte(defaultValue), &value); err != nil {
			return nil, err
		}
		if !valueMatchesType(value, typeName) {
			return nil, ex.Newf("default must encode a YAML value of type %q", typeName)
		}
		return value, nil
	default:
		return nil, ex.Newf("unsupported application configuration default type %q", typeName)
	}
}

func filterSelectedApplicationOptions(
	application ApplicationConfiguration,
	optionValues map[string]any,
	selected, catalog map[string]Entry,
	modulesWithoutMetadata []string,
) (map[string]any, []ApplicationConfigurationWarning, error) {
	filtered := make(map[string]any)
	var warnings []ApplicationConfigurationWarning
	selectedGoGroups := selectedGoConfigurationGroups(selected)
	for _, path := range slices.Sorted(maps.Keys(optionValues)) {
		value := optionValues[path]
		if _, ok := selected[path]; !ok {
			warning, err := classifyUnselectedApplicationOption(
				application,
				path,
				catalog,
				selectedGoGroups,
				modulesWithoutMetadata,
			)
			if err != nil {
				return nil, nil, err
			}
			warnings = append(warnings, warning)
			continue
		}
		if err := setApplicationOption(filtered, path, value); err != nil {
			return nil, nil, err
		}
	}
	for _, section := range slices.Sorted(maps.Keys(application.Instrumentation)) {
		value := application.Instrumentation[section]
		if section == generalConfigurationSection || section == goConfigurationSection || !hasApplicationValue(value) {
			continue
		}
		path := joinYAMLPath(instrumentationDevelopmentPath, section)
		warnings = append(warnings, ApplicationConfigurationWarning{
			Path:     path,
			Location: application.SourceLocations[path],
			Message:  "configuration is ignored because otelc only validates general and Go instrumentation options",
		})
	}
	return filtered, warnings, nil
}

func classifyUnselectedApplicationOption(
	application ApplicationConfiguration,
	path string,
	catalog map[string]Entry,
	selectedGoGroups map[string]struct{},
	modulesWithoutMetadata []string,
) (ApplicationConfigurationWarning, error) {
	location := application.SourceLocations[path]
	if strings.HasPrefix(path, generalConfigurationSection+".") {
		if _, known := catalog[path]; !known {
			return ApplicationConfigurationWarning{}, applicationConfigurationError(
				application,
				path,
				"unknown or removed configuration path",
			)
		}
		if len(modulesWithoutMetadata) > 0 {
			return ApplicationConfigurationWarning{}, missingModuleMetadataError(
				application,
				path,
				modulesWithoutMetadata,
			)
		}
		return ApplicationConfigurationWarning{
			Path:     path,
			Location: location,
			Message:  "configuration is ignored because no selected instrumentation declares this shared option",
		}, nil
	}
	if group, isGoOption := goConfigurationGroup(path); isGoOption {
		if _, groupSelected := selectedGoGroups[group]; groupSelected {
			return ApplicationConfigurationWarning{}, applicationConfigurationError(
				application,
				path,
				"unknown, removed, or renamed option for selected Go instrumentation",
			)
		}
		if len(modulesWithoutMetadata) > 0 {
			return ApplicationConfigurationWarning{}, missingModuleMetadataError(
				application,
				path,
				modulesWithoutMetadata,
			)
		}
		return ApplicationConfigurationWarning{
			Path:     path,
			Location: location,
			Message:  "configuration is ignored because the Go instrumentation is not selected",
		}, nil
	}
	return ApplicationConfigurationWarning{}, applicationConfigurationError(
		application,
		path,
		"unsupported instrumentation configuration path",
	)
}

func selectedGoConfigurationGroups(selected map[string]Entry) map[string]struct{} {
	groups := make(map[string]struct{})
	for name := range selected {
		if group, ok := goConfigurationGroup(name); ok {
			groups[group] = struct{}{}
		}
	}
	return groups
}

func parseAndValidateSelectedApplicationConfiguration(
	application ApplicationConfiguration,
	selectedDevelopment map[string]any,
	selected map[string]Entry,
) (*otelconfx.OpenTelemetryConfiguration, map[string]any, error) {
	sourceDocument := selectedApplicationSourceDocument(application.Document, selectedDevelopment, selected)
	if err := validateYAMLFields(
		sourceDocument,
		reflect.TypeFor[otelconfx.OpenTelemetryConfiguration](),
		application.Source,
		"",
	); err != nil {
		return nil, nil, explainDeprecatedApplicationField(err, application, selected)
	}

	var root map[string]any
	if err := application.Document.Decode(&root); err != nil {
		return nil, nil, ex.Wrapf(err, "decode application configuration %s", application.Source)
	}
	delete(root, instrumentationDevelopmentPath)
	if len(selectedDevelopment) > 0 {
		root[instrumentationDevelopmentPath] = selectedDevelopment
	}
	data, err := yaml.Marshal(root)
	if err != nil {
		return nil, nil, ex.Wrapf(err, "normalize application configuration %s", application.Source)
	}
	document, err := parseSingleYAMLDocument(data, application.Source)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := otelconfx.ParseYAML(data)
	if err != nil {
		return nil, nil, ex.Wrapf(err, "parse application configuration %s", application.Source)
	}
	if err = validateFileFormat(parsed.FileFormat); err != nil {
		return nil, nil, ex.Wrapf(err, "%s", application.Source)
	}
	if err = overlayEnvironmentValues(document, parsed, application.Source); err != nil {
		return nil, nil, err
	}
	fields, err := yamlNodeMapping(document, application.Source)
	if err != nil {
		return nil, nil, err
	}
	var normalized map[string]any
	if instrumentationNode := fields[instrumentationDevelopmentPath]; instrumentationNode != nil {
		if err = instrumentationNode.Decode(&normalized); err != nil {
			return nil, nil, ex.Wrapf(err, "decode selected instrumentation configuration %s", application.Source)
		}
	}
	return parsed, normalized, nil
}

// selectedApplicationSourceDocument returns a source-preserving view of the
// application document with unselected development options removed. Field
// validation uses this view so errors report positions from the user's YAML,
// while the normalized document passed to otelconf/x remains independent.
func selectedApplicationSourceDocument(
	source *yaml.Node,
	selectedDevelopment map[string]any,
	selected map[string]Entry,
) *yaml.Node {
	result := cloneYAMLNode(source)
	if result == nil || result.Kind != yaml.MappingNode {
		return result
	}
	for i := 0; i+1 < len(result.Content); i += yamlMappingPairSize {
		key := result.Content[i]
		if key.Kind != yaml.ScalarNode || key.Value != instrumentationDevelopmentPath {
			continue
		}
		result.Content[i+1] = filterSelectedYAMLNode(
			source.Content[i+1],
			selectedDevelopment,
			"",
			selected,
		)
		break
	}
	return result
}

func filterSelectedYAMLNode(
	source *yaml.Node,
	selectedValue any,
	path string,
	selected map[string]Entry,
) *yaml.Node {
	if source == nil {
		return nil
	}
	if source.Kind == yaml.AliasNode {
		filtered := *source
		filtered.Alias = filterSelectedYAMLNode(source.Alias, selectedValue, path, selected)
		return &filtered
	}
	values, ok := selectedValue.(map[string]any)
	if source.Kind != yaml.MappingNode || !ok {
		return cloneYAMLNode(source)
	}
	filtered := *source
	filtered.Content = make([]*yaml.Node, 0, len(source.Content))
	for i := 0; i+1 < len(source.Content); i += yamlMappingPairSize {
		key := source.Content[i]
		if key.Kind != yaml.ScalarNode {
			continue
		}
		value, exists := values[key.Value]
		if !exists {
			continue
		}
		childPath := key.Value
		if path != "" {
			childPath = path + "." + key.Value
		}
		filtered.Content = append(filtered.Content, cloneYAMLNode(key))
		if _, isOption := selected[childPath]; isOption {
			filtered.Content = append(filtered.Content, cloneYAMLNode(source.Content[i+1]))
			continue
		}
		filtered.Content = append(filtered.Content, filterSelectedYAMLNode(
			source.Content[i+1], value, childPath, selected,
		))
	}
	return &filtered
}

func cloneYAMLNode(source *yaml.Node) *yaml.Node {
	if source == nil {
		return nil
	}
	cloned := *source
	cloned.Content = make([]*yaml.Node, len(source.Content))
	for i, child := range source.Content {
		cloned.Content[i] = cloneYAMLNode(child)
	}
	return &cloned
}

func validateSelectedApplicationValues(
	application ApplicationConfiguration,
	normalizedDevelopment map[string]any,
	selected map[string]Entry,
) (map[string]any, []ApplicationConfigurationWarning, error) {
	values := ApplicationConfiguration{Instrumentation: normalizedDevelopment}.ApplicationOptionValues(selected)
	paths := slices.Sorted(maps.Keys(values))
	var warnings []ApplicationConfigurationWarning
	for _, path := range paths {
		definition := selected[path]
		location := application.SourceLocations[path]
		source := fmt.Sprintf("%s:%d:%d (%s)", location.File, location.Line, location.Column, path)
		if err := ValidateApplicationValue(definition, values[path], source); err != nil {
			if definition.Deprecated {
				if definition.ReplacedBy != nil {
					return nil, nil, ex.Newf(
						"%s:%d:%d: deprecated configuration %q is not valid; migrate to %q",
						location.File,
						location.Line,
						location.Column,
						path,
						*definition.ReplacedBy,
					)
				}
				return nil, nil, ex.Newf(
					"%s:%d:%d: deprecated configuration %q is not valid",
					location.File,
					location.Line,
					location.Column,
					path,
				)
			}
			return nil, nil, err
		}
		if definition.Deprecated {
			message := "selected configuration option is deprecated"
			if definition.ReplacedBy != nil {
				message += "; use " + *definition.ReplacedBy
			}
			warnings = append(warnings, ApplicationConfigurationWarning{
				Path:     path,
				Location: location,
				Message:  message,
			})
		}
	}
	return values, warnings, nil
}

func setApplicationOption(root map[string]any, path string, value any) error {
	parts := strings.Split(path, ".")
	if len(parts) < minimumConfigurationParts ||
		(parts[0] != generalConfigurationSection && parts[0] != goConfigurationSection) {
		return ex.Newf("unsupported instrumentation configuration path %q", path)
	}
	current := root
	for _, key := range parts[:len(parts)-1] {
		next, ok := current[key]
		if !ok {
			child := make(map[string]any)
			current[key] = child
			current = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			return ex.Newf("configuration path %q conflicts with a scalar value at %q", path, key)
		}
		current = child
	}
	current[parts[len(parts)-1]] = value
	return nil
}

func hasApplicationValue(value any) bool {
	switch value := value.(type) {
	case nil:
		return false
	case map[string]any:
		return len(value) > 0
	case []any:
		return len(value) > 0
	default:
		return true
	}
}

// goConfigurationGroup treats the first segment below go. as the
// instrumentation group. A group is selected only when selected metadata
// declares at least one option in it; configuration for other groups is
// classified as unselected.
func goConfigurationGroup(path string) (string, bool) {
	if !strings.HasPrefix(path, "go.") {
		return "", false
	}
	name := strings.TrimPrefix(path, "go.")
	group, _, ok := strings.Cut(name, ".")
	if !ok || group == "" {
		return "", false
	}
	return group, true
}

func applicationConfigurationError(application ApplicationConfiguration, path, message string) error {
	location := application.SourceLocations[path]
	return ex.Newf("%s:%d:%d: %s %q", location.File, location.Line, location.Column, message, path)
}

func explainDeprecatedApplicationField(
	err error,
	application ApplicationConfiguration,
	selected map[string]Entry,
) error {
	if unsupported, ok := errors.AsType[*unsupportedApplicationFieldError](err); ok {
		unsupportedPath := strings.TrimPrefix(unsupported.Path, "instrumentation/development.")
		for _, path := range slices.Sorted(maps.Keys(selected)) {
			definition := selected[path]
			location, hasLocation := application.SourceLocations[path]
			if !hasLocation || (path != unsupportedPath && !strings.HasPrefix(path, unsupportedPath+".")) ||
				!definition.Deprecated {
				continue
			}
			if definition.ReplacedBy != nil {
				return ex.Newf(
					"%s:%d:%d: deprecated configuration %q is not supported by pinned otelconf/x; use %q",
					application.Source,
					location.Line,
					location.Column,
					path,
					*definition.ReplacedBy,
				)
			}
			return ex.Newf(
				"%s:%d:%d: deprecated configuration %q is not supported by pinned otelconf/x",
				application.Source,
				location.Line,
				location.Column,
				path,
			)
		}
	}
	return err
}

func missingModuleMetadataError(
	application ApplicationConfiguration,
	path string,
	moduleRoots []string,
) error {
	location := application.SourceLocations[path]
	return ex.Newf(
		"%s:%d:%d: cannot validate configuration %q because selected instrumentation modules lack metadata.yaml: %s",
		location.File,
		location.Line,
		location.Column,
		path,
		strings.Join(moduleRoots, ", "),
	)
}

func indexInstrumentationLocations(root *yaml.Node, source string, locations map[string]SourceLocation) {
	fields, err := yamlNodeMapping(root, source)
	if err != nil {
		return
	}
	section := fields[instrumentationDevelopmentPath]
	for section != nil && section.Kind == yaml.AliasNode {
		section = section.Alias
	}
	if section == nil || section.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(section.Content); i += yamlMappingPairSize {
		key := section.Content[i]
		if key.Value == "general" || key.Value == "go" {
			indexYAMLNodeLocations(section.Content[i+1], key.Value, source, locations)
		} else {
			path := joinYAMLPath(instrumentationDevelopmentPath, key.Value)
			indexYAMLNodeLocations(section.Content[i+1], path, source, locations)
		}
	}
}

func indexYAMLNodeLocations(node *yaml.Node, path, source string, locations map[string]SourceLocation) {
	indexYAMLNodeLocationsOnPath(node, path, source, locations, make(map[*yaml.Node]struct{}))
}

func indexYAMLNodeLocationsOnPath(
	node *yaml.Node,
	path, source string,
	locations map[string]SourceLocation,
	active map[*yaml.Node]struct{},
) {
	if node == nil {
		return
	}
	locations[path] = SourceLocation{File: source, Line: node.Line, Column: node.Column}
	indexYAMLNodeLocationContents(node, path, source, locations, active)
}

func indexYAMLNodeLocationContents(
	node *yaml.Node,
	path, source string,
	locations map[string]SourceLocation,
	active map[*yaml.Node]struct{},
) {
	if node == nil {
		return
	}
	if _, found := active[node]; found {
		return
	}
	active[node] = struct{}{}
	defer delete(active, node)
	if node.Kind == yaml.AliasNode {
		indexYAMLNodeLocationContents(node.Alias, path, source, locations, active)
		return
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += yamlMappingPairSize {
			key := node.Content[i]
			indexYAMLNodeLocationsOnPath(
				node.Content[i+1], joinYAMLPath(path, key.Value), source, locations, active,
			)
		}
		return
	}
	if node.Kind == yaml.SequenceNode {
		for i, child := range node.Content {
			indexYAMLNodeLocationsOnPath(child, fmt.Sprintf("%s[%d]", path, i), source, locations, active)
		}
	}
}

func collectApplicationOptionValues(value any, path string, definitions map[string]Entry, values map[string]any) {
	if _, known := definitions[path]; known {
		values[path] = value
		return
	}
	if mapping, ok := value.(map[string]any); ok {
		keys := slices.Sorted(maps.Keys(mapping))
		for _, key := range keys {
			collectApplicationOptionValues(mapping[key], joinYAMLPath(path, key), definitions, values)
		}
		return
	}
	values[path] = value
}

// validateApplicationPathSegments rejects dotted keys used as path segments
// below general and go. Option paths are represented by nested YAML mappings;
// interpreting a literal dotted key as multiple segments can make source-node
// filtering disagree with the flattened option path and silently drop a value.
// Once a definition identifies a complete option, its value is opaque here so
// dots in map-valued options remain ordinary data.
func validateApplicationPathSegments(
	application ApplicationConfiguration,
	definitions map[string]Entry,
) error {
	root, err := yamlNodeMapping(application.Document, application.Source)
	if err != nil {
		return err
	}
	development := root[instrumentationDevelopmentPath]
	for development != nil && development.Kind == yaml.AliasNode {
		development = development.Alias
	}
	sections, err := yamlNodeMapping(development, application.Source)
	if err != nil {
		if development == nil {
			return nil
		}
		return err
	}
	for _, section := range []string{generalConfigurationSection, goConfigurationSection} {
		if err = validateApplicationPathNode(
			sections[section], applicationPath{
				option:     section,
				diagnostic: joinYAMLPath(instrumentationDevelopmentPath, section),
			},
			application.Source, definitions, make(map[*yaml.Node]struct{}),
		); err != nil {
			return err
		}
	}
	return nil
}

type applicationPath struct {
	option     string
	diagnostic string
}

func validateApplicationPathNode(
	node *yaml.Node,
	path applicationPath,
	source string,
	definitions map[string]Entry,
	active map[*yaml.Node]struct{},
) error {
	if node == nil {
		return nil
	}
	if _, found := active[node]; found {
		return ex.Newf("%s:%d:%d: %s: cyclic YAML alias", source, node.Line, node.Column, path.diagnostic)
	}
	active[node] = struct{}{}
	defer delete(active, node)
	if node.Kind == yaml.AliasNode {
		return validateApplicationPathNode(node.Alias, path, source, definitions, active)
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	if _, isOptionValue := definitions[path.option]; isOptionValue {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += yamlMappingPairSize {
		key := node.Content[i]
		if key.Kind != yaml.ScalarNode || key.Tag != yamlStringTag {
			continue
		}
		if strings.Contains(key.Value, ".") {
			return ex.Newf(
				"%s:%d:%d: configuration path key %q contains a dot; use nested YAML mappings",
				source,
				key.Line,
				key.Column,
				key.Value,
			)
		}
		childPath := applicationPath{
			option:     joinYAMLPath(path.option, key.Value),
			diagnostic: joinYAMLPath(path.diagnostic, key.Value),
		}
		if _, isOptionValue := definitions[childPath.option]; isOptionValue {
			continue
		}
		if err := validateApplicationPathNode(
			node.Content[i+1], childPath, source, definitions, active,
		); err != nil {
			return err
		}
	}
	return nil
}

func parseSingleYAMLDocument(data []byte, source string) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := decoder.Decode(&root); err != nil {
		return nil, ex.Wrapf(err, "parse application configuration %s", source)
	}
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, ex.Newf("%s: application configuration must be a single mapping", source)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil {
		return nil, ex.Newf("%s: application configuration must contain a single YAML document", source)
	} else if !errors.Is(err, io.EOF) {
		return nil, ex.Wrapf(err, "parse trailing application configuration document %s", source)
	}
	return root.Content[0], nil
}

func rejectDuplicateYAMLKeys(node *yaml.Node, source string) error {
	return rejectDuplicateYAMLKeysOnPath(node, source, "", make(map[*yaml.Node]struct{}))
}

func rejectDuplicateYAMLKeysOnPath(
	node *yaml.Node,
	source, path string,
	active map[*yaml.Node]struct{},
) error {
	if node == nil {
		return nil
	}
	if _, found := active[node]; found {
		return ex.Newf("%s:%d:%d: %s: cyclic YAML alias", source, node.Line, node.Column, path)
	}
	active[node] = struct{}{}
	defer delete(active, node)
	if node.Kind == yaml.AliasNode {
		return rejectDuplicateYAMLKeysOnPath(node.Alias, source, path, active)
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]*yaml.Node, len(node.Content)/yamlMappingPairSize)
		for i := 0; i+1 < len(node.Content); i += yamlMappingPairSize {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != yamlStringTag {
				return ex.Newf("%s:%d:%d: %s: mapping keys must be strings", source, key.Line, key.Column, path)
			}
			if previous, ok := seen[key.Value]; ok {
				return ex.Newf(
					"%s:%d:%d: %s: duplicate key %q (first declared at line %d)",
					source,
					key.Line,
					key.Column,
					path,
					key.Value,
					previous.Line,
				)
			}
			seen[key.Value] = key
			childPath := joinYAMLPath(path, key.Value)
			if err := rejectDuplicateYAMLKeysOnPath(node.Content[i+1], source, childPath, active); err != nil {
				return err
			}
		}
		return nil
	}
	if node.Kind == yaml.SequenceNode {
		for i, child := range node.Content {
			if err := rejectDuplicateYAMLKeysOnPath(child, source, fmt.Sprintf("%s[%d]", path, i), active); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateYAMLFields(node *yaml.Node, target reflect.Type, source, path string) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.AliasNode {
		return validateYAMLFields(node.Alias, target, source, path)
	}
	if node.Kind == yaml.MappingNode {
		return validateYAMLMappingFields(node, dereferenceYAMLType(target), source, path)
	}
	if node.Kind == yaml.SequenceNode {
		return validateYAMLSequenceFields(node, dereferenceYAMLType(target), source, path)
	}
	return nil
}

func dereferenceYAMLType(target reflect.Type) reflect.Type {
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	return target
}

func validateYAMLMappingFields(node *yaml.Node, target reflect.Type, source, path string) error {
	if target.Kind() == reflect.Struct {
		return validateYAMLStructFields(node, target, source, path)
	}
	if target.Kind() == reflect.Map {
		return validateYAMLMapFields(node, target, source, path)
	}
	return nil
}

func validateYAMLStructFields(node *yaml.Node, target reflect.Type, source, path string) error {
	fieldSet, fieldSetErr := yamlStructFields(target)
	if fieldSetErr != nil {
		return ex.Wrapf(fieldSetErr, "inspect YAML fields for %s", target)
	}
	for i := 0; i+1 < len(node.Content); i += yamlMappingPairSize {
		key := node.Content[i]
		if key.Kind != yaml.ScalarNode || key.Tag != yamlStringTag {
			continue // rejectDuplicateYAMLKeys reports this with source location.
		}
		fieldType, ok := fieldSet.fields[key.Value]
		if !ok {
			if fieldSet.inlineMap != nil {
				if err := validateYAMLFields(
					node.Content[i+1],
					fieldSet.inlineMap.Elem(),
					source,
					joinYAMLPath(path, key.Value),
				); err != nil {
					return err
				}
				continue
			}
			if fieldSet.inlineUnmarshaler {
				continue
			}
			return &unsupportedApplicationFieldError{
				Source: source,
				Path:   joinYAMLPath(path, key.Value),
				Field:  key.Value,
				Line:   key.Line,
				Column: key.Column,
			}
		}
		if err := validateYAMLFields(node.Content[i+1], fieldType, source, joinYAMLPath(path, key.Value)); err != nil {
			return err
		}
	}
	return nil
}

func validateYAMLMapFields(node *yaml.Node, target reflect.Type, source, path string) error {
	keyKind := target.Key().Kind()
	if keyKind != reflect.Interface && keyKind != reflect.String {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += yamlMappingPairSize {
		key := node.Content[i]
		if err := validateYAMLFields(
			node.Content[i+1],
			target.Elem(),
			source,
			joinYAMLPath(path, key.Value),
		); err != nil {
			return err
		}
	}
	return nil
}

func validateYAMLSequenceFields(node *yaml.Node, target reflect.Type, source, path string) error {
	if target.Kind() != reflect.Slice && target.Kind() != reflect.Array {
		return nil
	}
	for i, child := range node.Content {
		if err := validateYAMLFields(child, target.Elem(), source, fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

type unsupportedApplicationFieldError struct {
	Source string
	Path   string
	Field  string
	Line   int
	Column int
}

func (e *unsupportedApplicationFieldError) Error() string {
	return fmt.Sprintf(
		"%s:%d:%d: unsupported field %q in pinned otelconf/x model at %s",
		e.Source,
		e.Line,
		e.Column,
		e.Field,
		e.Path,
	)
}

func validateApplicationRootFields(node *yaml.Node, target reflect.Type, source string) error {
	if node == nil || node.Kind != yaml.MappingNode {
		return ex.Newf("%s: application configuration must be a mapping", source)
	}
	fieldSet, fieldSetErr := yamlStructFields(target)
	if fieldSetErr != nil {
		return ex.Wrapf(fieldSetErr, "inspect YAML fields for %s", target)
	}
	for i := 0; i+1 < len(node.Content); i += yamlMappingPairSize {
		key := node.Content[i]
		if key.Kind != yaml.ScalarNode || key.Tag != yamlStringTag {
			continue // rejectDuplicateYAMLKeys reports this with source location.
		}
		if key.Value == instrumentationDevelopmentPath {
			// Selected-only validation filters this subtree before parsing it with
			// otelconf/x, so unknown or invalid unselected options can be ignored.
			continue
		}
		fieldType, ok := fieldSet.fields[key.Value]
		if !ok {
			if fieldSet.inlineMap != nil {
				if err := validateYAMLFields(
					node.Content[i+1],
					fieldSet.inlineMap.Elem(),
					source,
					key.Value,
				); err != nil {
					return err
				}
				continue
			}
			if fieldSet.inlineUnmarshaler {
				continue
			}
			return ex.Newf(
				"%s:%d:%d: unsupported application configuration field %q",
				source,
				key.Line,
				key.Column,
				key.Value,
			)
		}
		if err := validateYAMLFields(node.Content[i+1], fieldType, source, key.Value); err != nil {
			return err
		}
	}
	return nil
}

type yamlStructFieldSet struct {
	fields            map[string]reflect.Type
	fieldOrder        []string
	inlineMap         reflect.Type
	inlineUnmarshaler bool
}

func yamlStructFields(target reflect.Type) (yamlStructFieldSet, error) {
	target = dereferenceYAMLType(target)
	if target.Kind() != reflect.Struct {
		return yamlStructFieldSet{}, nil
	}
	fieldSet := yamlStructFieldSet{fields: make(map[string]reflect.Type, target.NumField())}
	for field := range target.Fields() {
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		fieldInfo, tagErr := yamlStructFieldTag(field, target)
		if tagErr != nil {
			return yamlStructFieldSet{}, tagErr
		}
		if fieldInfo.skip {
			continue
		}
		if fieldInfo.inline {
			if inlineErr := mergeYAMLInlineField(&fieldSet, field.Type, target); inlineErr != nil {
				return yamlStructFieldSet{}, inlineErr
			}
			continue
		}
		if fieldInfo.name == "" {
			fieldInfo.name = strings.ToLower(field.Name)
		}
		if _, duplicate := fieldSet.fields[fieldInfo.name]; duplicate {
			return yamlStructFieldSet{}, ex.Newf("duplicated key %q in YAML struct %s", fieldInfo.name, target)
		}
		fieldSet.fields[fieldInfo.name] = field.Type
		fieldSet.fieldOrder = append(fieldSet.fieldOrder, fieldInfo.name)
	}
	return fieldSet, nil
}

type yamlStructFieldTagInfo struct {
	name   string
	inline bool
	skip   bool
}

func yamlStructFieldTag(field reflect.StructField, target reflect.Type) (yamlStructFieldTagInfo, error) {
	tag := field.Tag.Get("yaml")
	if tag == "" && !strings.Contains(string(field.Tag), ":") {
		tag = string(field.Tag)
	}
	if tag == "-" {
		return yamlStructFieldTagInfo{skip: true}, nil
	}
	parts := strings.Split(tag, ",")
	inline := false
	for _, flag := range parts[1:] {
		switch flag {
		case "omitempty", "flow":
		case "inline":
			inline = true
		default:
			return yamlStructFieldTagInfo{}, ex.Newf(
				"unsupported flag %q in YAML tag %q of type %s",
				flag,
				tag,
				target,
			)
		}
	}
	return yamlStructFieldTagInfo{name: parts[0], inline: inline}, nil
}

func mergeYAMLInlineField(fieldSet *yamlStructFieldSet, fieldType, target reflect.Type) error {
	if fieldType.Kind() == reflect.Map {
		if fieldSet.inlineMap != nil {
			return ex.Newf("multiple inline maps in YAML struct %s", target)
		}
		if fieldType.Key() != reflect.TypeFor[string]() {
			return ex.Newf("inline map in YAML struct %s must have string keys", target)
		}
		fieldSet.inlineMap = fieldType
		return nil
	}
	if fieldType.Kind() == reflect.Struct || fieldType.Kind() == reflect.Pointer {
		inlineType := dereferenceYAMLType(fieldType)
		if inlineType.Kind() != reflect.Struct {
			return ex.Newf("inline field in YAML struct %s must be a struct or map", target)
		}
		if reflect.PointerTo(inlineType).Implements(reflect.TypeFor[yaml.Unmarshaler]()) {
			fieldSet.inlineUnmarshaler = true
			return nil
		}
		inlineFields, err := yamlStructFields(inlineType)
		if err != nil {
			return err
		}
		return mergeYAMLInlineStruct(fieldSet, inlineFields, target)
	}
	return ex.Newf("inline field in YAML struct %s must be a struct or map", target)
}

func mergeYAMLInlineStruct(fieldSet *yamlStructFieldSet, inlineFields yamlStructFieldSet, target reflect.Type) error {
	for _, key := range inlineFields.fieldOrder {
		fieldType := inlineFields.fields[key]
		if _, duplicate := fieldSet.fields[key]; duplicate {
			return ex.Newf("duplicated key %q in YAML struct %s", key, target)
		}
		fieldSet.fields[key] = fieldType
		fieldSet.fieldOrder = append(fieldSet.fieldOrder, key)
	}
	fieldSet.inlineUnmarshaler = fieldSet.inlineUnmarshaler || inlineFields.inlineUnmarshaler
	return nil
}

func validateFileFormat(fileFormat string) error {
	majorMinor, suffix, hasSuffix := strings.Cut(fileFormat, "-")
	validSuffixCharacters := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-"
	if hasSuffix && (suffix == "" || strings.Trim(suffix, validSuffixCharacters) != "") {
		return ex.Newf("file_format %q has an invalid prerelease suffix", fileFormat)
	}
	majorText, minorText, ok := strings.Cut(majorMinor, ".")
	if !ok || minorText == "" || strings.Contains(minorText, ".") {
		return ex.Newf("file_format %q must be major.minor", fileFormat)
	}
	major, majorErr := strconv.Atoi(majorText)
	minor, minorErr := strconv.Atoi(minorText)
	if majorErr != nil || minorErr != nil || major < 0 || minor < 0 {
		return ex.Newf("file_format %q must be major.minor", fileFormat)
	}
	supported, ok := schemaMajorMinor(OpenTelemetryConfigurationRelease)
	if !ok {
		return ex.Newf("invalid pinned OpenTelemetry Configuration release %q", OpenTelemetryConfigurationRelease)
	}
	if major != supported.major {
		return ex.Newf(
			"file_format %q has unsupported major version; otelc supports major %d",
			fileFormat,
			supported.major,
		)
	}
	if minor > supported.minor {
		return ex.Newf(
			"file_format %q is newer than the supported OpenTelemetry Configuration %s schema",
			fileFormat,
			OpenTelemetryConfigurationRelease,
		)
	}
	return nil
}

type majorMinorVersion struct {
	major int
	minor int
}

func schemaMajorMinor(release string) (majorMinorVersion, bool) {
	release = strings.TrimPrefix(release, "v")
	parts := strings.Split(release, ".")
	if len(parts) < minimumConfigurationParts {
		return majorMinorVersion{}, false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	if majorErr != nil || minorErr != nil {
		return majorMinorVersion{}, false
	}
	return majorMinorVersion{major: major, minor: minor}, true
}

func hasYAMLField(node *yaml.Node, name string) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += yamlMappingPairSize {
		if node.Content[i].Value == name {
			return true
		}
	}
	return false
}

func joinYAMLPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}
