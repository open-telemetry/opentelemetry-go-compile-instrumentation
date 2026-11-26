// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"reflect"
	"strconv"
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
	assert.Equal(t, []any{"x-request-id"}, validated.ResolvedValues["general.http.client.request_captured_headers"])
	assert.Equal(t, true, validated.ResolvedValues["go.my_instrumentation.enabled"])
	assert.NotEmpty(t, validated.Snapshot)
	assert.NotEmpty(t, validated.Identity)
}

func TestValidateApplicationOptionsAppliesMetadataDefaults(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  go:
    sample:
      labels: []
`), "app.yaml")
	require.NoError(t, err)
	validated, err := ValidateApplicationOptions(app, []Entry{
		{Name: "go.sample.enabled", Type: booleanType, Default: new("true")},
		{Name: "go.sample.labels", Type: listType},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, true, validated.ResolvedValues["go.sample.enabled"])
	assert.Equal(t, []any{}, validated.ResolvedValues["go.sample.labels"])
	assert.NotContains(t, validated.Values, "go.sample.enabled")
}

func TestValidateApplicationOptionsResolvesTypedMetadataDefaults(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  go:
    sample: {}
`), "app.yaml")
	require.NoError(t, err)

	tests := []struct {
		name         string
		metadata     string
		option       string
		want         any
		wantSnapshot string
	}{
		{
			name: "boolean numeric spelling",
			metadata: `configurations:
  - name: go.sample.enabled
    type: boolean
    default: "1"
`,
			option:       "go.sample.enabled",
			want:         true,
			wantSnapshot: `{"go.sample.enabled":true}`,
		},
		{
			name: "boolean letter spelling",
			metadata: `configurations:
  - name: go.sample.enabled
    type: boolean
    default: "t"
`,
			option:       "go.sample.enabled",
			want:         true,
			wantSnapshot: `{"go.sample.enabled":true}`,
		},
		{
			name: "integer leading zero is decimal",
			metadata: `configurations:
  - name: go.sample.limit
    type: int
    default: "010"
`,
			option:       "go.sample.limit",
			want:         int(10),
			wantSnapshot: `{"go.sample.limit":10}`,
		},
		{
			name: "string numeric spelling remains unchanged",
			metadata: `configurations:
  - name: go.sample.mode
    type: string
    default: "010"
`,
			option:       "go.sample.mode",
			want:         "010",
			wantSnapshot: `{"go.sample.mode":"010"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metadata, parseErr := ParseMetadata([]byte(test.metadata), "metadata.yaml")
			require.NoError(t, parseErr)

			validated, validateErr := ValidateApplicationOptions(app, metadata.Entries, nil)
			require.NoError(t, validateErr)
			assert.Equal(t, test.want, validated.ResolvedValues[test.option])
			assert.NotContains(t, validated.Values, test.option)
			assert.JSONEq(t, test.wantSnapshot, string(validated.Snapshot))
		})
	}
}

func TestApplicationIntegerDefaultMatchesSuppliedYAMLValue(t *testing.T) {
	defaultApplication, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  go:
    sample: {}
`), "default.yaml")
	require.NoError(t, err)
	suppliedApplication, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  go:
    sample:
      limit: 10
`), "supplied.yaml")
	require.NoError(t, err)
	definition := Entry{Name: "go.sample.limit", Type: integerType, Default: new("010")}

	defaultResult, err := ValidateApplicationOptions(defaultApplication, []Entry{definition}, nil)
	require.NoError(t, err)
	suppliedResult, err := ValidateApplicationOptions(suppliedApplication, []Entry{definition}, nil)
	require.NoError(t, err)

	assert.Equal(t, int(10), defaultResult.ResolvedValues[definition.Name])
	assert.Equal(t, reflect.TypeFor[int](), reflect.TypeOf(defaultResult.ResolvedValues[definition.Name]))
	assert.Equal(t, suppliedResult.Values[definition.Name], defaultResult.ResolvedValues[definition.Name])
	assert.Equal(
		t,
		reflect.TypeOf(suppliedResult.Values[definition.Name]),
		reflect.TypeOf(defaultResult.ResolvedValues[definition.Name]),
	)
	assert.Equal(t, suppliedResult.Snapshot, defaultResult.Snapshot)
	assert.Equal(t, suppliedResult.Identity, defaultResult.Identity)
}

func TestApplicationIntegerDefaultUsesInt64WhenNativeIntCannotRepresentIt(t *testing.T) {
	for _, value := range []string{"-9223372036854775808", "9223372036854775807"} {
		t.Run(value, func(t *testing.T) {
			got, err := applicationDefaultValue(value, integerType)
			require.NoError(t, err)
			parsed, err := strconv.ParseInt(value, 10, 64)
			require.NoError(t, err)
			native := int(parsed)
			if int64(native) == parsed {
				assert.Equal(t, native, got)
				assert.Equal(t, reflect.TypeFor[int](), reflect.TypeOf(got))
			} else {
				assert.Equal(t, parsed, got)
				assert.Equal(t, reflect.TypeFor[int64](), reflect.TypeOf(got))
			}
		})
	}
}

func TestValidateApplicationOptionsAppliesStringDefault(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  go:
    sample: {}
`), "app.yaml")
	require.NoError(t, err)

	validated, err := ValidateApplicationOptions(app, []Entry{{
		Name:    "go.sample.mode",
		Type:    stringType,
		Default: new("client"),
	}}, nil)
	require.NoError(t, err)
	assert.Equal(t, "client", validated.ResolvedValues["go.sample.mode"])
}

func TestValidateApplicationOptionsRejectsMalformedMetadataDefault(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
`), "app.yaml")
	require.NoError(t, err)

	_, err = ValidateApplicationOptions(app, []Entry{{
		Name:    "go.sample.values",
		Type:    listType,
		Default: new("["),
	}}, nil)
	require.ErrorContains(t, err, "decode default for go.sample.values")
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
			name: "unknown nested SDK field",
			data: "file_format: '1.2'\nresource:\n  unknown_sdk_field: true\n",
			want: `unsupported field "unknown_sdk_field" in pinned otelconf/x model`,
		},
		{
			name: "invalid SDK value shape",
			data: "file_format: '1.2'\nresource: []\n",
			want: "parse SDK configuration",
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
			name: "selected list violates generated upstream constraints",
			data: "file_format: '1.2'\ninstrumentation/development:\n  general:\n    http:\n      client:\n        request_captured_headers: []\n",
			want: "must contain at least 1 items",
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
			if strings.Contains(test.name, "generated upstream constraints") {
				catalog, catalogErr := CatalogDefinitions()
				require.NoError(t, catalogErr)
				selected = []Entry{catalog["general.http.client.request_captured_headers"]}
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
	catalog, err := CatalogDefinitions()
	require.NoError(t, err)
	definition := catalog["general.http.client.request_captured_headers"]
	definition.Deprecated = true
	definition.ReplacedBy = &replacement

	_, err = ValidateApplicationOptions(app, []Entry{definition}, nil)
	require.ErrorContains(t, err, "migrate to")
	require.ErrorContains(t, err, replacement)
}

func TestValidateApplicationOptionsExplainsInvalidDeprecatedOptionWithoutReplacement(t *testing.T) {
	app, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  general:
    http:
      client:
        request_captured_headers: []
`), "app.yaml")
	require.NoError(t, err)
	catalog, err := CatalogDefinitions()
	require.NoError(t, err)
	definition := catalog["general.http.client.request_captured_headers"]
	definition.Deprecated = true

	_, err = ValidateApplicationOptions(app, []Entry{definition}, nil)
	require.ErrorContains(
		t,
		err,
		`deprecated configuration "general.http.client.request_captured_headers" is not valid`,
	)
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

func TestApplicationOptionValuesHandlesNilAndUnknownNestedValues(t *testing.T) {
	assert.Empty(t, (ApplicationConfiguration{}).ApplicationOptionValues(nil))

	values := (ApplicationConfiguration{
		Instrumentation: map[string]any{
			"general": map[string]any{
				"known":  true,
				"nested": map[string]any{"value": "x"},
			},
		},
	}).ApplicationOptionValues(map[string]Entry{
		"general.known": {Name: "general.known", Type: booleanType},
	})
	assert.Equal(t, true, values["general.known"])
	assert.Equal(t, "x", values["general.nested.value"])
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

func TestValidateApplicationValueEnforcesCatalogConstraints(t *testing.T) {
	minItems := 1
	listEntry := Entry{
		Name: "general.http.client.request_captured_headers",
		Type: listType,
		Validation: &ValueConstraints{
			MinItems:  &minItems,
			ItemsType: stringType,
		},
	}
	require.NoError(t, ValidateApplicationValue(listEntry, []any{"x-request-id"}, "app.yaml:1:1"))
	require.ErrorContains(
		t,
		ValidateApplicationValue(listEntry, []any{}, "app.yaml:1:1"),
		"must contain at least 1 items",
	)
	require.ErrorContains(
		t,
		ValidateApplicationValue(listEntry, []any{1}, "app.yaml:1:1"),
		"item 0",
	)

	minimum := float64(0)
	intEntry := Entry{
		Name:       "general.http.semconv.version",
		Type:       integerType,
		Validation: &ValueConstraints{Minimum: &minimum},
	}
	require.NoError(t, ValidateApplicationValue(intEntry, 0, "app.yaml:1:1"))
	require.ErrorContains(
		t,
		ValidateApplicationValue(intEntry, -1, "app.yaml:1:1"),
		"must be at least 0",
	)
}

func TestValidateApplicationValueEnforcesMaximumAndCountOnlyConstraints(t *testing.T) {
	maximum := float64(2)
	intEntry := Entry{
		Name:       "general.http.semconv.version",
		Type:       integerType,
		Validation: &ValueConstraints{Maximum: &maximum},
	}
	require.NoError(t, ValidateApplicationValue(intEntry, 2, "app.yaml:1:1"))
	require.ErrorContains(
		t,
		ValidateApplicationValue(intEntry, 3, "app.yaml:1:1"),
		"must be at most 2",
	)

	minItems, maxItems := 0, 2
	listEntry := Entry{
		Name: "go.sample.values",
		Type: listType,
		Validation: &ValueConstraints{
			MinItems: &minItems,
			MaxItems: &maxItems,
		},
	}
	require.NoError(t, ValidateApplicationValue(listEntry, []any{1, 2}, "app.yaml:1:1"))
	require.ErrorContains(
		t,
		ValidateApplicationValue(listEntry, []any{1, 2, 3}, "app.yaml:1:1"),
		"must contain at most 2 items",
	)
}

func TestValidateApplicationValueAcceptsAllNativeIntegerTypes(t *testing.T) {
	minimum := float64(0)
	entry := Entry{
		Name:       "go.sample.value",
		Type:       integerType,
		Validation: &ValueConstraints{Minimum: &minimum},
	}
	for _, value := range []any{
		int8(0),
		int16(0),
		int32(0),
		int64(0),
		uint(0),
		uint8(0),
		uint16(0),
		uint32(0),
		uint64(0),
	} {
		require.NoError(t, ValidateApplicationValue(entry, value, "app.yaml:1:1"))
	}
	_, ok := integerNumber(float64(1))
	assert.False(t, ok)
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

func TestValidateApplicationOptionsRejectsSelectedInvalidValue(t *testing.T) {
	application, err := ParseApplicationConfiguration([]byte(`file_format: "1.2"
instrumentation/development:
  go:
    selected:
      enabled: not-a-boolean
`), "app.yaml")
	require.NoError(t, err)

	_, err = ValidateApplicationOptions(application, []Entry{{
		Name: "go.selected.enabled",
		Type: booleanType,
	}}, nil)
	require.ErrorContains(t, err, "go.selected.enabled")
}

func TestValidateApplicationOptionsRejectsSelectedUnsupportedPath(t *testing.T) {
	application := ApplicationConfiguration{
		Source:          "app.yaml",
		Instrumentation: map[string]any{"go": true},
		SourceLocations: map[string]SourceLocation{"go": {File: "app.yaml", Line: 2, Column: 3}},
	}

	_, err := ValidateApplicationOptions(application, []Entry{{Name: "go", Type: booleanType}}, nil)
	require.ErrorContains(t, err, "unsupported instrumentation configuration path")
}

func TestValidateApplicationOptionsReportsOriginalSDKFieldLocation(t *testing.T) {
	var document yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(`file_format: "1.2"

resource:
  unknown_sdk_field: true

instrumentation/development:
  go:
    sample:
      enabled: true
`), &document))
	application := ApplicationConfiguration{Source: "app.yaml", Document: document.Content[0]}
	_, _, err := parseAndValidateSelectedApplicationConfiguration(
		application,
		map[string]any{"go": map[string]any{"sample": map[string]any{"enabled": true}}},
		map[string]Entry{"go.sample.enabled": {Name: "go.sample.enabled", Type: booleanType}},
	)
	require.ErrorContains(t, err, `app.yaml:4:3: unsupported field "unknown_sdk_field"`)
	require.ErrorContains(t, err, "resource.unknown_sdk_field")
}

func TestSelectedApplicationSourceDocumentFiltersWithOriginalNodes(t *testing.T) {
	var document yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(`file_format: "1.2"
instrumentation/development:
  defaults: &options
    sample:
      enabled: true
    ignored:
      stale: true
  go: *options
`), &document))
	selectedDevelopment := map[string]any{"go": map[string]any{"sample": map[string]any{"enabled": true}}}
	selected := map[string]Entry{"go.sample.enabled": {Name: "go.sample.enabled", Type: booleanType}}

	sourceDocument := selectedApplicationSourceDocument(document.Content[0], selectedDevelopment, selected)
	sourceRoot, err := yamlNodeMapping(document.Content[0], "app.yaml")
	require.NoError(t, err)
	filteredRoot, err := yamlNodeMapping(sourceDocument, "app.yaml")
	require.NoError(t, err)
	originalDevelopment, err := yamlNodeMapping(sourceRoot["instrumentation/development"], "app.yaml")
	require.NoError(t, err)
	filteredDevelopment, err := yamlNodeMapping(filteredRoot["instrumentation/development"], "app.yaml")
	require.NoError(t, err)
	originalGo, err := yamlNodeMapping(originalDevelopment["go"].Alias, "app.yaml")
	require.NoError(t, err)
	filteredGo, err := yamlNodeMapping(filteredDevelopment["go"].Alias, "app.yaml")
	require.NoError(t, err)

	assert.Contains(t, originalGo, "ignored")
	assert.NotContains(t, filteredGo, "ignored")
	assert.Equal(t, originalGo["sample"].Line, filteredGo["sample"].Line)
}

func TestApplicationDefaultValueRejectsMalformedYAML(t *testing.T) {
	_, err := applicationDefaultValue("[", listType)
	require.Error(t, err)
	value, err := applicationDefaultValue("client", stringType)
	require.NoError(t, err)
	assert.Equal(t, "client", value)
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

	scalar := &yaml.Node{Kind: yaml.ScalarNode, Value: "literal"}
	assert.NoError(t, overlayEnvironmentNode(scalar, parsed, "app.yaml", "root"))

	var sourceMapping, parsedMapping yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("known: ${VALUE}\nmissing: literal\n"), &sourceMapping))
	require.NoError(t, yaml.Unmarshal([]byte("known: value\n"), &parsedMapping))
	assert.NoError(t, overlayEnvironmentNode(
		sourceMapping.Content[0], parsedMapping.Content[0], "app.yaml", "root",
	))
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

	application.Document = &yaml.Node{Kind: yaml.ScalarNode, Value: "invalid"}
	_, _, err = parseAndValidateSelectedApplicationConfiguration(application, nil, nil)
	require.ErrorContains(t, err, "decode application configuration")
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
	assert.NoError(t, validateYAMLFields(
		&yaml.Node{Kind: yaml.SequenceNode}, reflect.TypeFor[int](), "app.yaml", "",
	))

	type Embedded struct {
		Value string `yaml:"value"`
	}
	type config struct {
		Embedded
	}
	var embeddedNode, inlineNode yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("embedded:\n  value: true\n"), &embeddedNode))
	require.NoError(t, validateYAMLFields(embeddedNode.Content[0], reflect.TypeFor[config](), "app.yaml", ""))
	require.NoError(t, yaml.Unmarshal([]byte("value: true\n"), &inlineNode))
	var unknownNode yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("unknown: true\n"), &unknownNode))
	require.ErrorContains(t, validateYAMLFields(
		inlineNode.Content[0], reflect.TypeFor[config](), "app.yaml", ""), "unsupported field")
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

func TestApplicationValidationHelperBranches(t *testing.T) {
	sequence := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "value"},
	}}
	assert.NoError(t, validateYAMLSequenceFields(sequence, reflect.TypeFor[int](), "app.yaml", "items"))

	nonStringKey := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.SequenceNode}, {Kind: yaml.ScalarNode, Value: "value"},
	}}
	assert.NoError(t, validateYAMLStructFields(
		nonStringKey, reflect.TypeFor[struct{}](), "app.yaml", "",
	))

	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "instrumentation/development"},
		{Kind: yaml.MappingNode},
	}}
	assert.NoError(t, validateApplicationRootFields(
		root, reflect.TypeFor[struct{}](), "app.yaml",
	))

	application := ApplicationConfiguration{
		Source:          "app.yaml",
		SourceLocations: map[string]SourceLocation{"go.sample.option": {File: "app.yaml", Line: 2, Column: 3}},
	}
	err := explainDeprecatedApplicationField(&unsupportedApplicationFieldError{
		Path: "instrumentation/development.go.sample.option",
	}, application, map[string]Entry{
		"go.sample.option": {Name: "go.sample.option", Deprecated: true},
	})
	require.ErrorContains(t, err, "deprecated configuration")
}

func TestYAMLStructFieldsSkipsUnexportedAndIgnoredFields(t *testing.T) {
	fieldSet, err := yamlStructFields(reflect.TypeFor[struct {
		Visible string `yaml:"visible"`
		Ignored string `yaml:"-"`
		hidden  string
	}]())
	require.NoError(t, err)
	assert.Contains(t, fieldSet.fields, "visible")
	assert.NotContains(t, fieldSet.fields, "Ignored")
	assert.NotContains(t, fieldSet.fields, "hidden")
}

func TestValidateYAMLFieldsMatchesYAMLV3StructFieldSemantics(t *testing.T) {
	type Embedded struct {
		Value string `yaml:"value"`
	}
	type inlineConfig struct {
		Embedded `yaml:",inline"`
	}
	type pointerInlineConfig struct {
		*Embedded `yaml:",inline"`
	}
	type mapInlineConfig struct {
		Known  string            `yaml:"known"`
		Extras map[string]string `yaml:",inline"`
	}
	type nestedMapInlineConfig struct {
		Extras map[string]Embedded `yaml:",inline"`
	}

	for _, test := range []struct {
		name   string
		data   string
		typeOf reflect.Type
		want   string
	}{
		{name: "lowercases the full untagged acronym name", data: "url: value\n", typeOf: reflect.TypeFor[struct{ URL string }]()},
		{name: "flattens only explicitly inline structs", data: "value: item\n", typeOf: reflect.TypeFor[inlineConfig]()},
		{name: "supports pointers to inline structs", data: "value: item\n", typeOf: reflect.TypeFor[pointerInlineConfig]()},
		{name: "accepts unknown fields through an inline map", data: "known: item\nextra: value\n", typeOf: reflect.TypeFor[mapInlineConfig]()},
		{name: "validates values in inline maps", data: "extra:\n  unknown: true\n", typeOf: reflect.TypeFor[nestedMapInlineConfig](), want: "unsupported field"},
		{name: "rejects acronym with lowercased first letter only", data: "uRL: value\n", typeOf: reflect.TypeFor[struct{ URL string }](), want: "unsupported field"},
		{name: "rejects untagged embedded-field flattening", data: "value: item\n", typeOf: reflect.TypeFor[struct{ Embedded }](), want: "unsupported field"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var document yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(test.data), &document))
			err := validateYAMLFields(document.Content[0], test.typeOf, "app.yaml", "")
			if test.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.want)
			}
		})
	}

	type DuplicateInline struct {
		Value    string   `yaml:"value"`
		Embedded Embedded `yaml:",inline"`
	}
	_, err := yamlStructFields(reflect.TypeFor[DuplicateInline]())
	require.ErrorContains(t, err, "duplicated key")

	type InvalidInlineMap struct {
		Values map[int]string `yaml:",inline"`
	}
	_, err = yamlStructFields(reflect.TypeFor[InvalidInlineMap]())
	require.ErrorContains(t, err, "must have string keys")
}

func TestValidateApplicationRootFieldsHandlesNilAndInvalidKeys(t *testing.T) {
	require.Error(t, validateApplicationRootFields(nil, reflect.TypeFor[struct{}](), "app.yaml"))
	nonStringKey := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.SequenceNode}, {Kind: yaml.ScalarNode, Value: "value"},
	}}
	assert.NoError(t, validateApplicationRootFields(nonStringKey, reflect.TypeFor[struct{}](), "app.yaml"))
}
