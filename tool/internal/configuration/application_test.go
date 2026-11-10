// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
