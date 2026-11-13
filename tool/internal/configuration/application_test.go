// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	otelconfx "go.opentelemetry.io/contrib/otelconf/x"
	"go.yaml.in/yaml/v3"
)

func TestParseApplicationConfiguration(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  general:
    http:
      client:
        request_captured_headers: [x-request-id]
  go:
    my_instrumentation:
      enabled: true
`), "app.yaml")
	require.NoError(t, err)
	catalog, err := CatalogDefinitions()
	require.NoError(t, err)
	validated, err := ValidateApplicationOptions(app, []Entry{
		catalog["general.http.client.request_captured_headers"],
		{Name: "go.my_instrumentation.enabled", Type: booleanType},
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, validated.Parsed)
	assert.Equal(t, "1.2", validated.Parsed.FileFormat)
	assert.Equal(t, []any{"x-request-id"}, validated.Values["general.http.client.request_captured_headers"])
	assert.Equal(t, true, validated.Values["go.my_instrumentation.enabled"])
}

func TestParseApplicationConfigurationExpandsEnvironmentDuringSetup(t *testing.T) {
	t.Setenv("OTELC_TEST_FILE_FORMAT", "1.2")
	t.Setenv("OTELC_TEST_ENABLED", "false")
	app, err := ParseApplicationConfiguration([]byte(`file_format: ${OTELC_TEST_FILE_FORMAT}
instrumentation/development:
  go:
    sample:
      enabled: ${OTELC_TEST_ENABLED}
`), "app.yaml")
	require.NoError(t, err)
	validated, err := ValidateApplicationOptions(app, []Entry{{Name: "go.sample.enabled", Type: booleanType}}, nil)
	require.NoError(t, err)
	assert.Equal(t, "1.2", validated.Parsed.FileFormat)
	assert.Equal(t, false, validated.Values["go.sample.enabled"])
}

func TestValidateApplicationOptionsPreservesFalseAndEmptyCollection(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  go:
    sample:
      enabled: false
      labels: []
`), "app.yaml")
	require.NoError(t, err)

	validated, err := ValidateApplicationOptions(app, []Entry{
		{Name: "go.sample.enabled", Type: booleanType},
		{Name: "go.sample.labels", Type: listType},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, false, validated.Values["go.sample.enabled"])
	assert.Equal(t, []any{}, validated.Values["go.sample.labels"])
}

func TestParseApplicationConfigurationRejectsInvalidDocuments(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "non-mapping document",
			data: "[]",
			want: "application configuration must be a single mapping",
		},
		{
			name: "duplicate root key",
			data: "file_format: '1.2'\nfile_format: '1.2'\n",
			want: "duplicate key \"file_format\"",
		},
		{
			name: "multiple documents",
			data: "file_format: '1.2'\n---\nfile_format: '1.2'\n",
			want: "single YAML document",
		},
		{
			name: "unknown SDK field",
			data: "file_format: '1.2'\nunknown_sdk_field: true\n",
			want: "unsupported application configuration field \"unknown_sdk_field\"",
		},
		{
			name: "missing file format",
			data: "disabled: false\n",
			want: "file_format",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseApplicationConfiguration([]byte(test.data), "app.yaml")
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestValidateApplicationConfigurationFileFormatAndPinnedParser(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
		want string
	}{
		{
			name: "unsupported major version",
			data: "file_format: '2.0'\n",
			want: "unsupported major version",
		},
		{
			name: "newer minor version",
			data: "file_format: '1.3'\n",
			want: "newer than the supported",
		},
		{
			name: "malformed prerelease version",
			data: "file_format: '1.2-'\n",
			want: "invalid prerelease suffix",
		},
		{
			name: "selected v1.2 field absent from pinned parser",
			data: "file_format: '1.2'\ninstrumentation/development:\n  general:\n    sanitization:\n      url:\n        sensitive_query_parameters: [token]\n",
			want: "unsupported field \"sanitization\" in pinned otelconf/x model",
		},
		{
			name: "selected value violates pinned upstream schema",
			data: "file_format: '1.2'\ninstrumentation/development:\n  general:\n    http:\n      client:\n        request_captured_headers: []\n",
			want: "validate instrumentation/development against OpenTelemetry Configuration",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, err := ParseApplicationConfiguration([]byte(test.data), "app.yaml")
			if err != nil {
				require.ErrorContains(t, err, test.want)
				return
			}
			var selected []Entry
			if strings.Contains(test.name, "v1.2 field") {
				selected = []Entry{{
					Name: "general.sanitization.url.sensitive_query_parameters",
					Type: listType,
				}}
			}
			if strings.Contains(test.name, "schema") {
				selected = []Entry{{
					Name: "general.http.client.request_captured_headers",
					Type: listType,
				}}
			}
			_, err = ValidateApplicationOptions(app, selected, nil)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestParseApplicationConfigurationRejectsNullInstrumentationSection(t *testing.T) {
	_, err := ParseApplicationConfiguration(
		[]byte("file_format: '1.2'\ninstrumentation/development: null\n"),
		"app.yaml",
	)
	require.ErrorContains(t, err, "instrumentation/development must be an object")
}

func TestValidateApplicationOptions(t *testing.T) {
	catalog, err := CatalogDefinitions()
	require.NoError(t, err)
	definition := catalog["general.http.client.request_captured_headers"]
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  general:
    http:
      client:
        request_captured_headers: [x-request-id]
`), "app.yaml")
	require.NoError(t, err)

	validated, err := ValidateApplicationOptions(app, []Entry{definition}, nil)
	require.NoError(t, err)
	assert.Equal(t, []any{"x-request-id"}, validated.Values[definition.Name])
	assert.Empty(t, validated.Warnings)
}

func TestValidateApplicationOptionsWarnsForUnselectedConfiguration(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  go:
    unselected:
      enabled: true
`), "app.yaml")
	require.NoError(t, err)

	validated, err := ValidateApplicationOptions(app, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, validated.Values)
	require.Len(t, validated.Warnings, 1)
	assert.Equal(t, "go.unselected.enabled", validated.Warnings[0].Path)
	assert.Equal(t, "app.yaml", validated.Warnings[0].Location.File)
}

func TestValidateApplicationOptionsUsesMetadataToSelectGoGroups(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  go:
    unselected:
      enabled: true
`), "app.yaml")
	require.NoError(t, err)

	validated, err := ValidateApplicationOptions(app, []Entry{{
		Name: "go.selected.enabled",
		Type: booleanType,
	}}, nil)
	require.NoError(t, err)
	assert.Empty(t, validated.Values)
	require.Len(t, validated.Warnings, 1)
	assert.Equal(t, "go.unselected.enabled", validated.Warnings[0].Path)
}

func TestValidateApplicationOptionsWarnsForUnselectedGeneralOption(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  general:
    http:
      client:
        request_captured_headers: [x-request-id]
`), "app.yaml")
	require.NoError(t, err)

	validated, err := ValidateApplicationOptions(app, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, validated.Values)
	require.Len(t, validated.Warnings, 1)
	assert.Equal(t, "general.http.client.request_captured_headers", validated.Warnings[0].Path)
}

func TestValidateApplicationOptionsIgnoresUnselectedFieldOutsidePinnedParser(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  general:
    http:
      client:
        known_methods: [GET, POST]
`), "app.yaml")
	require.NoError(t, err)

	validated, err := ValidateApplicationOptions(app, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, validated.Values)
	require.Len(t, validated.Warnings, 1)
	assert.Equal(t, "general.http.client.known_methods", validated.Warnings[0].Path)
}

func TestValidateApplicationOptionsRejectsUnknownSelectedGoOption(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  go:
    selected:
      unknown: true
`), "app.yaml")
	require.NoError(t, err)

	_, err = ValidateApplicationOptions(app, []Entry{{Name: "go.selected.enabled", Type: booleanType}}, nil)
	require.ErrorContains(
		t,
		err,
		`unknown, removed, or renamed option for selected Go instrumentation "go.selected.unknown"`,
	)
}

func TestValidateApplicationOptionsReportsMissingMetadataForUnresolvedGoOption(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  go:
    selected:
      enabled: true
`), "app.yaml")
	require.NoError(t, err)

	_, err = ValidateApplicationOptions(app, nil, []string{"/instrumentation/selected"})
	require.ErrorContains(t, err, `cannot validate configuration "go.selected.enabled"`)
	require.ErrorContains(t, err, "/instrumentation/selected")
}

func TestValidateApplicationOptionsWarnsForDeprecatedOption(t *testing.T) {
	replacement := "general.http.client.request_captured_headers"
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  general:
    http:
      client:
        request_captured_headers: [x-request-id]
`), "app.yaml")
	require.NoError(t, err)

	validated, err := ValidateApplicationOptions(app, []Entry{{
		Name:       "general.http.client.request_captured_headers",
		Type:       listType,
		Deprecated: true,
		ReplacedBy: &replacement,
	}}, nil)
	require.NoError(t, err)
	require.Len(t, validated.Warnings, 1)
	assert.Contains(t, validated.Warnings[0].Message, "deprecated")
	assert.Contains(t, validated.Warnings[0].Message, replacement)
}

func TestValidateApplicationOptionsExplainsDeprecatedOptionMissingFromParser(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  general:
    sanitization:
      url:
        sensitive_query_parameters: [token]
`), "app.yaml")
	require.NoError(t, err)
	replacement := "general.http.client.request_captured_headers"

	_, err = ValidateApplicationOptions(app, []Entry{{
		Name:       "general.sanitization.url.sensitive_query_parameters",
		Type:       listType,
		Deprecated: true,
		ReplacedBy: &replacement,
	}}, nil)

	require.ErrorContains(t, err, "deprecated configuration")
	require.ErrorContains(t, err, replacement)
}

func TestValidateApplicationOptionsExplainsDeprecatedSchemaOption(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  general:
    http:
      client:
        request_captured_headers: []
`), "app.yaml")
	require.NoError(t, err)
	replacement := "general.http.client.request_captured_headers.v2"

	_, err = ValidateApplicationOptions(app, []Entry{{
		Name:       "general.http.client.request_captured_headers",
		Type:       listType,
		Deprecated: true,
		ReplacedBy: &replacement,
	}}, nil)
	require.ErrorContains(t, err, "migrate to")
	require.ErrorContains(t, err, replacement)
}

func TestApplicationHelpers(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
		want  bool
	}{
		{name: "nil", value: nil},
		{name: "empty map", value: map[string]any{}},
		{name: "empty list", value: []any{}},
		{name: "scalar", value: "value", want: true},
		{name: "non-empty map", value: map[string]any{"key": true}, want: true},
		{name: "non-empty list", value: []any{"value"}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, hasApplicationValue(test.value))
		})
	}

	assert.Equal(t, "general.http", joinYAMLPath("general", "http"))
	assert.Equal(t, "http", joinYAMLPath("", "http"))
	for _, test := range []struct {
		path  string
		group string
		ok    bool
	}{
		{path: "general.http"},
		{path: "go"},
		{path: "go."},
		{path: "go.http.client", group: "http", ok: true},
	} {
		group, ok := goConfigurationGroup(test.path)
		assert.Equal(t, test.group, group)
		assert.Equal(t, test.ok, ok)
	}
}

func TestSetApplicationOptionErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{name: "unsupported root", path: "other.value", want: "unsupported instrumentation configuration path"},
		{name: "missing leaf", path: "go", want: "unsupported instrumentation configuration path"},
		{name: "scalar conflict", path: "go.http.client", want: "conflicts with a scalar"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := map[string]any{"go": map[string]any{"http": "scalar"}}
			if test.name == "scalar conflict" {
				root = map[string]any{"go": map[string]any{"http": "scalar"}}
			}
			err := setApplicationOption(root, test.path, true)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestApplicationYAMLHelpers(t *testing.T) {
	var document yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(`root:
  nested:
    - value
`), &document))
	root := document.Content[0]

	fields, err := yamlNodeMapping(root, "app.yaml")
	require.NoError(t, err)
	assert.Contains(t, fields, "root")
	require.NoError(t, validateYAMLFields(
		root,
		reflect.TypeFor[struct {
			Root map[string][]string `yaml:"root"`
		}](),
		"app.yaml",
		"",
	))

	_, err = yamlNodeMapping(&yaml.Node{Kind: yaml.ScalarNode}, "app.yaml")
	require.ErrorContains(t, err, "must be a mapping")
	assert.True(t, hasYAMLField(root, "root"))
	assert.False(t, hasYAMLField(root, "missing"))
}

func TestValidateApplicationValue(t *testing.T) {
	entry := Entry{
		Name: "go.http.rules",
		Type: structuredListType,
		Schema: &Schema{
			Type:     "object",
			Required: []string{"enabled"},
			Properties: map[string]Property{
				"enabled": {Type: booleanType},
			},
		},
	}

	require.NoError(t, ValidateApplicationValue(entry, []any{map[string]any{"enabled": true}}, "app.yaml:1:1"))
	for _, value := range []any{
		"not-a-list",
		[]any{"not-a-map"},
		[]any{map[string]any{}},
		[]any{map[string]any{"unknown": true}},
		[]any{map[string]any{"enabled": "true"}},
	} {
		require.Error(t, ValidateApplicationValue(entry, value, "app.yaml:1:1"))
	}
	require.Error(t, ValidateApplicationValue(Entry{Name: "go.enabled", Type: booleanType}, "true", "app.yaml:1:1"))
}

func TestOverlayEnvironmentValuesRejectsInvalidNormalizedShapes(t *testing.T) {
	var source, parsed yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("value: ${VALUE}\n"), &source))
	require.NoError(t, yaml.Unmarshal([]byte("value:\n  nested: true\n"), &parsed))
	sourceRoot := source.Content[0]
	parsedRoot := parsed.Content[0]

	err := overlayEnvironmentNode(sourceRoot, parsedRoot, "app.yaml", "")
	require.ErrorContains(t, err, "environment substitution at value did not produce a scalar")
}

func TestOverlayEnvironmentValuesRejectsNonMappingSource(t *testing.T) {
	var parsed otelconfx.OpenTelemetryConfiguration
	require.NoError(t, yaml.Unmarshal([]byte("file_format: '1.2'\n"), &parsed))

	err := overlayEnvironmentValues(&yaml.Node{Kind: yaml.ScalarNode}, &parsed, "app.yaml")
	require.ErrorContains(t, err, "application configuration must contain one mapping")
}

func TestOverlayEnvironmentSequenceReportsSubstitutionErrors(t *testing.T) {
	var source, parsed yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("value: [\"${VALUE}\"]\n"), &source))
	require.NoError(t, yaml.Unmarshal([]byte("value: [{nested: true}]\n"), &parsed))

	err := overlayEnvironmentNode(source.Content[0], parsed.Content[0], "app.yaml", "")
	require.ErrorContains(t, err, "environment substitution at value[0] did not produce a scalar")
}

func TestOverlayEnvironmentNodeHandlesAliasesAndShortSequences(t *testing.T) {
	var source, parsed yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("value: &item [\"${ONE}\", \"${TWO}\"]\n"), &source))
	require.NoError(t, yaml.Unmarshal([]byte("value: [one]\n"), &parsed))

	err := overlayEnvironmentNode(source.Content[0], parsed.Content[0], "app.yaml", "")
	require.NoError(t, err)
	assert.Equal(t, "one", source.Content[0].Content[1].Content[0].Value)

	alias := &yaml.Node{Kind: yaml.AliasNode, Alias: &yaml.Node{Kind: yaml.ScalarNode, Value: "${VALUE}"}}
	parsedScalar := &yaml.Node{Kind: yaml.ScalarNode, Value: "one"}
	require.NoError(t, overlayEnvironmentNode(alias, parsedScalar, "app.yaml", "value"))
	assert.Equal(t, "one", alias.Alias.Value)
}

func TestValidateApplicationOptionsClassifiesUnknownAndIgnoredSections(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  general:
    unknown:
      enabled: true
  custom:
    enabled: true
`), "app.yaml")
	require.NoError(t, err)

	_, err = ValidateApplicationOptions(app, nil, nil)
	require.ErrorContains(t, err, "unknown or removed configuration path")

	app, err = ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  custom:
    enabled: true
`), "app.yaml")
	require.NoError(t, err)
	validated, err := ValidateApplicationOptions(app, nil, nil)
	require.NoError(t, err)
	require.Len(t, validated.Warnings, 1)
	assert.Contains(t, validated.Warnings[0].Message, "only validates general and Go")
}

func TestValidateApplicationOptionsRejectsUnsupportedSectionPath(t *testing.T) {
	application := ApplicationConfiguration{
		Source:          "app.yaml",
		Instrumentation: map[string]any{"go": true},
		SourceLocations: map[string]SourceLocation{"go": {File: "app.yaml", Line: 2, Column: 3}},
	}

	_, err := ValidateApplicationOptions(application, nil, nil)
	require.ErrorContains(t, err, "unsupported instrumentation configuration path")
}

func TestValidateApplicationOptionsReportsMissingMetadataForGeneralOption(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  general:
    http:
      client:
        request_captured_headers: [x-request-id]
`), "app.yaml")
	require.NoError(t, err)

	_, err = ValidateApplicationOptions(app, nil, []string{"/instrumentation/http"})
	require.ErrorContains(t, err, "selected instrumentation modules lack metadata.yaml")
}

func TestValidateYAMLFieldsCoversMapsSequencesAndPointers(t *testing.T) {
	var document yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("items:\n  - name: value\n"), &document))
	type item struct {
		Name string `yaml:"name"`
	}
	type config struct {
		Items []item `yaml:"items"`
	}
	require.NoError(t, validateYAMLFields(
		document.Content[0],
		reflect.TypeFor[*config](),
		"app.yaml",
		"",
	))

	var invalid yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("items:\n  - unknown: true\n"), &invalid))
	require.ErrorContains(t, validateYAMLFields(
		invalid.Content[0],
		reflect.TypeFor[config](),
		"app.yaml",
		"",
	), "unsupported field")
}

func TestValidateFileFormatAndSchemaMajorMinorRejectMalformedValues(t *testing.T) {
	for _, value := range []string{"", "1", "1.", "1.2.3", "x.2", "-1.2", "1.2-", "1.2-!"} {
		require.Error(t, validateFileFormat(value), value)
	}
	for _, value := range []string{"", "1", "x.y"} {
		_, ok := schemaMajorMinor(value)
		assert.False(t, ok, value)
	}
	version, ok := schemaMajorMinor("v1.2.3")
	require.True(t, ok)
	assert.Equal(t, majorMinorVersion{major: 1, minor: 2}, version)
}

func TestYAMLNodeMappingRejectsNilAndScalarNodes(t *testing.T) {
	_, err := yamlNodeMapping(nil, "app.yaml")
	require.ErrorContains(t, err, "application configuration must be a mapping")
	_, err = yamlNodeMapping(&yaml.Node{Kind: yaml.ScalarNode}, "app.yaml")
	require.ErrorContains(t, err, "application configuration must be a mapping")
}

func TestYAMLTraversalHandlesNilAndNonMappingNodes(t *testing.T) {
	locations := map[string]SourceLocation{}
	indexInstrumentationLocations(nil, "app.yaml", locations)
	indexInstrumentationLocations(&yaml.Node{Kind: yaml.ScalarNode}, "app.yaml", locations)
	indexYAMLNodeLocations(nil, "root", "app.yaml", locations)
	assert.Empty(t, locations)

	assert.False(t, hasYAMLField(nil, "field"))
	assert.False(t, hasYAMLField(&yaml.Node{Kind: yaml.ScalarNode}, "field"))
}

func TestOverlayEnvironmentNodeIgnoresIncompatibleNodes(t *testing.T) {
	source := &yaml.Node{Kind: yaml.MappingNode}
	parsed := &yaml.Node{Kind: yaml.ScalarNode, Value: "value"}
	assert.NoError(t, overlayEnvironmentNode(source, parsed, "app.yaml", "root"))
	assert.NoError(t, overlayEnvironmentNode(nil, parsed, "app.yaml", "root"))
}

func TestParseSingleYAMLDocumentReportsTrailingErrors(t *testing.T) {
	_, err := parseSingleYAMLDocument([]byte("file_format: [\n"), "app.yaml")
	require.ErrorContains(t, err, "parse application configuration")

	_, err = parseSingleYAMLDocument([]byte("file_format: '1.2'\n---\n["), "app.yaml")
	require.ErrorContains(t, err, "parse trailing application configuration document")
}

func TestParseAndValidateSelectedConfigurationRejectsMalformedDocuments(t *testing.T) {
	var malformed, invalidFormat yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("file_format: {}\n"), &malformed))
	require.NoError(t, yaml.Unmarshal([]byte("file_format: '1.3'\n"), &invalidFormat))

	application := ApplicationConfiguration{Source: "app.yaml", Document: malformed.Content[0]}
	_, _, err := parseAndValidateSelectedApplicationConfiguration(application, nil, nil)
	require.Error(t, err)

	application.Document = invalidFormat.Content[0]
	_, _, err = parseAndValidateSelectedApplicationConfiguration(application, nil, nil)
	require.ErrorContains(t, err, "newer than the supported")
}

func TestRejectDuplicateYAMLKeysHandlesAliasesAndNonStringKeys(t *testing.T) {
	require.NoError(t, rejectDuplicateYAMLKeys(nil, "app.yaml", ""))
	var document yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("base: &base\n  value: true\ncopy: *base\n"), &document))
	require.NoError(t, rejectDuplicateYAMLKeys(document.Content[0], "app.yaml", ""))

	var nestedDuplicate yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("items:\n  - value: true\n    value: false\n"), &nestedDuplicate))
	require.ErrorContains(t, rejectDuplicateYAMLKeys(nestedDuplicate.Content[0], "app.yaml", ""), "duplicate key")

	var sequenceDuplicate yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("items:\n  - value: true\n"), &sequenceDuplicate))
	require.NoError(t, rejectDuplicateYAMLKeys(sequenceDuplicate.Content[0], "app.yaml", ""))

	nonStringKey := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.SequenceNode, Content: []*yaml.Node{{Kind: yaml.ScalarNode, Value: "key"}}},
		{Kind: yaml.ScalarNode, Value: "value"},
	}}
	require.ErrorContains(t, rejectDuplicateYAMLKeys(nonStringKey, "app.yaml", ""), "mapping keys must be strings")
}

func TestValidateYAMLFieldsHandlesUnsupportedShapes(t *testing.T) {
	assert.NoError(t, validateYAMLFields(nil, reflect.TypeFor[int](), "app.yaml", ""))
	assert.NoError(t, validateYAMLFields(
		&yaml.Node{Kind: yaml.AliasNode, Alias: &yaml.Node{Kind: yaml.ScalarNode}},
		reflect.TypeFor[int](),
		"app.yaml",
		"",
	))

	mapping := &yaml.Node{Kind: yaml.MappingNode}
	assert.NoError(t, validateYAMLFields(mapping, reflect.TypeFor[int](), "app.yaml", ""))

	mapNode := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "key"},
		{Kind: yaml.ScalarNode, Value: "value"},
	}}
	assert.NoError(t, validateYAMLFields(mapNode, reflect.TypeFor[map[string]string](), "app.yaml", ""))
	assert.NoError(t, validateYAMLFields(mapNode, reflect.TypeFor[map[int]string](), "app.yaml", ""))

	type Embedded struct {
		Value string `yaml:"value"`
	}
	type config struct {
		Embedded
	}
	var embeddedNode yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("value: true\n"), &embeddedNode))
	require.NoError(t, validateYAMLFields(embeddedNode.Content[0], reflect.TypeFor[config](), "app.yaml", ""))
	var unknownNode yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("unknown: true\n"), &unknownNode))
	require.ErrorContains(t, validateYAMLFields(
		unknownNode.Content[0], reflect.TypeFor[config](), "app.yaml", ""), "unsupported field")

	var nestedMap yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("items:\n  nested:\n    unknown: true\n"), &nestedMap))
	type mapConfig struct {
		Items map[string]config `yaml:"items"`
	}
	require.ErrorContains(t, validateYAMLFields(
		nestedMap.Content[0], reflect.TypeFor[mapConfig](), "app.yaml", ""), "unsupported field")
}

func TestYAMLStructFieldsSkipsUnexportedAndIgnoredFields(t *testing.T) {
	type config struct {
		Visible string `yaml:"visible"`
		Ignored string `yaml:"-"`
	}

	fields := yamlStructFields(reflect.TypeFor[config]())
	assert.Contains(t, fields, "visible")
	assert.NotContains(t, fields, "Ignored")
}

func TestValidateApplicationRootFieldsHandlesNilAndInvalidKeys(t *testing.T) {
	require.Error(t, validateApplicationRootFields(nil, reflect.TypeFor[struct{}](), "app.yaml"))
	nonStringKey := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.SequenceNode}, {Kind: yaml.ScalarNode, Value: "value"},
	}}
	assert.NoError(t, validateApplicationRootFields(nonStringKey, reflect.TypeFor[struct{}](), "app.yaml"))
}
