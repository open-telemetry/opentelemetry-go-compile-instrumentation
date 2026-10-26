// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMetadata(t *testing.T) {
	data := []byte(`name: test-instrumentation
description: ignored by the configuration parser
configurations:
  - name: otel.instrumentation.http.enabled
    declarative_name: go.net_http.enabled
    type: bool
    default: "true"
  - ref: general.http.client.request_captured_headers/development
  - name: legacy.go.option
    type: time.Duration
    default: 10s
  - name: legacy.boolean.option
    type: bool
    default: "true"
`)

	metadata, err := ParseMetadata(data, "/module/metadata.yaml")
	require.NoError(t, err)
	require.Len(t, metadata.Entries, 2, "legacy flat-only options are ignored by the declarative resolver")
	assert.Equal(t, "/module/metadata.yaml", metadata.Source)
	assert.Equal(t, "go.net_http.enabled", metadata.Entries[0].DeclarativeName)
	assert.Equal(t, "bool", metadata.Entries[0].Type)
	require.NotNil(t, metadata.Entries[0].Default)
	assert.Equal(t, "true", *metadata.Entries[0].Default)
	assert.Equal(t, "general.http.client.request_captured_headers/development", metadata.Entries[1].Ref)
}

func TestParseMetadataDistinguishesAbsentAndEmptyDefaults(t *testing.T) {
	metadata, err := ParseMetadata([]byte(`configurations:
  - declarative_name: general.no_default
    type: string
  - declarative_name: general.empty_default
    type: string
    default: ""
`), "metadata.yaml")
	require.NoError(t, err)
	require.Len(t, metadata.Entries, 2)
	assert.Nil(t, metadata.Entries[0].Default)
	require.NotNil(t, metadata.Entries[1].Default)
	assert.Empty(t, *metadata.Entries[1].Default)
}

func TestParseMetadataRejectsMalformedDeclarations(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "both selectors",
			body: `- declarative_name: go.net_http.enabled
  ref: general.http.client.request_captured_headers/development`,
			want: "exactly one of declarative_name or ref",
		},
		{
			name: "empty name",
			body: `- declarative_name: ""`,
			want: "declarative_name must not be empty",
		},
		{
			name: "empty ref",
			body: `- ref: ""`,
			want: "ref must not be empty",
		},
		{
			name: "surrounding name whitespace",
			body: `- declarative_name: " go.net_http.enabled "`,
			want: "declarative_name must not have leading or trailing whitespace",
		},
		{
			name: "surrounding ref whitespace",
			body: `- ref: " general.http.client.request_captured_headers/development "`,
			want: "ref must not have leading or trailing whitespace",
		},
		{
			name: "ref repeats inline data",
			body: `- ref: general.http.client.request_captured_headers/development
  description: duplicate`,
			want: "a ref entry must not include inline configuration fields",
		},
		{
			name: "ref repeats an empty inline field",
			body: `- ref: general.http.client.request_captured_headers/development
  description: ""`,
			want: "a ref entry must not include inline configuration fields",
		},
		{
			name: "unknown declarative type",
			body: `- declarative_name: go.net_http.enabled
  declarative_type: str`,
			want: `unsupported declarative_type "str"`,
		},
		{
			name: "declarative type without selector",
			body: `- declarative_type: boolean`,
			want: "declarative_type or declarative_schema requires declarative_name or ref",
		},
		{
			name: "non-scalar top-level default",
			body: `- declarative_name: go.net_http.enabled
  default: [true]`,
			want: "default must be a string, boolean, integer, or float scalar",
		},
		{
			name: "invalid boolean default",
			body: `- declarative_name: go.net_http.enabled
  declarative_type: boolean
  default: "yes"`,
			want: `default "yes" is not a valid boolean`,
		},
		{
			name: "schema on scalar",
			body: `- declarative_name: go.net_http.enabled
  declarative_type: boolean
  declarative_schema:
    type: object
    properties: {}`,
			want: "declarative_schema requires declarative_type structured_list",
		},
		{
			name: "structured list without schema",
			body: `- declarative_name: go.http.rules
  declarative_type: structured_list`,
			want: "structured_list requires declarative_schema",
		},
		{
			name: "schema type is not object",
			body: `- declarative_name: go.http.rules
  declarative_type: structured_list
  declarative_schema:
    type: string
    properties: {}`,
			want: "type must be object",
		},
		{
			name: "required property is missing",
			body: `- declarative_name: go.http.rules
  declarative_type: structured_list
  declarative_schema:
    type: object
    required: [missing]
    properties: {}`,
			want: `required key "missing" is not defined in properties`,
		},
		{
			name: "property default retains native value and is type checked",
			body: `- declarative_name: go.http.rules
  declarative_type: structured_list
  declarative_schema:
    type: object
    properties:
      enabled:
        type: boolean
        default: "false"`,
			want: "properties.enabled.default: value does not match type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte("configurations:\n" + indentYAML(tt.body, "  ") + "\n")
			_, err := ParseMetadata(body, "metadata.yaml")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestParseMetadataNormalizesTopLevelScalarDefaultsToStrings(t *testing.T) {
	metadata, err := ParseMetadata([]byte(`configurations:
  - declarative_name: go.feature.enabled
    declarative_type: boolean
    default: true
  - declarative_name: go.feature.limit
    declarative_type: int
    default: 17
`), "metadata.yaml")
	require.NoError(t, err)
	require.Len(t, metadata.Entries, 2)
	require.NotNil(t, metadata.Entries[0].Default)
	assert.Equal(t, "true", *metadata.Entries[0].Default)
	require.NotNil(t, metadata.Entries[1].Default)
	assert.Equal(t, "17", *metadata.Entries[1].Default)
}

func TestParseMetadataPropertyValuesRetainYAMLTypes(t *testing.T) {
	metadata, err := ParseMetadata([]byte(`configurations:
  - declarative_name: go.http.rules
    declarative_type: structured_list
    declarative_schema:
      type: object
      required: [enabled]
      properties:
        enabled:
          type: boolean
          default: false
          example: true
        pattern:
          type: string
          example: "/users/{id}"
`), "metadata.yaml")
	require.NoError(t, err)
	require.Len(t, metadata.Entries, 1)
	properties := metadata.Entries[0].DeclarativeSchema.Properties
	assert.Equal(t, false, properties["enabled"].Default)
	assert.Equal(t, true, properties["enabled"].Example)
	assert.Equal(t, "/users/{id}", properties["pattern"].Example)
}

func TestConfigurationCatalogAndResolver(t *testing.T) {
	definitions, err := CatalogDefinitions()
	require.NoError(t, err)
	assert.Equal(t, "v1.2.0", OpenTelemetryConfigurationRelease)
	assert.Equal(t, "3b04ae78576ec407854d734eb4da4f78a67f902c", OpenTelemetryConfigurationCommit)
	require.GreaterOrEqual(t, len(definitions), 26, "catalog contains the general.* leaves from the pinned schema")
	require.Contains(t, definitions, "general.http.client.request_captured_headers/development")
	assert.Equal(
		t,
		"general.http.client.request_captured_headers/development",
		definitions["general.http.client.request_captured_headers/development"].DeclarativeName,
	)
	assert.Equal(
		t,
		"No outbound request headers are captured.",
		definitions["general.http.client.request_captured_headers/development"].DefaultBehavior,
	)
	assert.Equal(t, "int", definitions["general.http.semconv.version/development"].DeclarativeType)
	assert.Equal(t, "boolean", definitions["general.http.semconv.experimental/development"].DeclarativeType)

	first, err := ParseMetadata([]byte(`configurations:
  - ref: general.http.client.request_captured_headers/development
`), "/a/metadata.yaml")
	require.NoError(t, err)
	second, err := ParseMetadata([]byte(`configurations:
  - ref: general.http.client.request_captured_headers/development
`), "/b/metadata.yaml")
	require.NoError(t, err)

	resolved, err := ResolveMetadata([]Metadata{first, second}, definitions)
	require.NoError(t, err)
	require.Len(t, resolved, 1, "identical references resolve once")
	assert.Equal(t, "general.http.client.request_captured_headers/development", resolved[0].Ref)
	assert.Equal(t, "general.http.client.request_captured_headers/development", resolved[0].DeclarativeName)
}

func TestParseCatalogAcceptsOtelcOwnedGoDefinitions(t *testing.T) {
	definitions, err := ParseCatalog([]byte(`catalog_version: 1
source:
  repository: open-telemetry/opentelemetry-configuration
  release: v1.2.0
  commit: 3b04ae78576ec407854d734eb4da4f78a67f902c
  file: schema/instrumentation.yaml
definitions:
  go.example.enabled:
    declarative_name: go.example.enabled
    description: Enables the example instrumentation.
    declarative_type: boolean
    default: "false"
`))
	require.NoError(t, err)
	require.Contains(t, definitions, "go.example.enabled")
	assert.Equal(t, "go.example.enabled", definitions["go.example.enabled"].DeclarativeName)
	assert.Equal(t, "boolean", definitions["go.example.enabled"].DeclarativeType)
	require.NotNil(t, definitions["go.example.enabled"].Default)
	assert.Equal(t, "false", *definitions["go.example.enabled"].Default)
}

func TestResolveMetadataReportsUnknownRefsAndConflictingPaths(t *testing.T) {
	definitions, err := CatalogDefinitions()
	require.NoError(t, err)

	unknown, err := ParseMetadata([]byte(`configurations:
  - ref: missing.definition
`), "/module/metadata.yaml")
	require.NoError(t, err)
	_, err = ResolveMetadata([]Metadata{unknown}, definitions)
	require.ErrorContains(t, err, `unknown configuration ref "missing.definition"`)

	left, err := ParseMetadata([]byte(`configurations:
  - declarative_name: go.net_http.enabled
    description: left
`), "/left/metadata.yaml")
	require.NoError(t, err)
	right, err := ParseMetadata([]byte(`configurations:
  - declarative_name: go.net_http.enabled
    description: right
`), "/right/metadata.yaml")
	require.NoError(t, err)
	_, err = ResolveMetadata([]Metadata{left, right}, definitions)
	require.ErrorContains(t, err, `conflicting configuration definitions for declarative_name "go.net_http.enabled"`)
}

func TestResolveMetadataReportsConflictingCatalogDefinitions(t *testing.T) {
	first, err := ParseMetadata([]byte(`configurations:
  - ref: first.definition
  - ref: second.definition
`), "/module/metadata.yaml")
	require.NoError(t, err)
	definitions := map[string]Entry{
		"first.definition": {
			Name:            "first",
			DeclarativeName: "go.shared.option",
			Type:            "boolean",
			Source:          "shared definition first.definition",
		},
		"second.definition": {
			Name:            "second",
			DeclarativeName: "go.shared.option",
			Type:            "boolean",
			Description:     "different definition",
			Source:          "shared definition second.definition",
		},
	}
	_, err = ResolveMetadata([]Metadata{first}, definitions)
	require.ErrorContains(t, err, `conflicting configuration definitions for declarative_name "go.shared.option"`)
}

func TestResolveMetadataDetectsEnvironmentNameCollisions(t *testing.T) {
	first, err := ParseMetadata([]byte(`configurations:
  - declarative_name: general.http.foo-bar
`), "/a/metadata.yaml")
	require.NoError(t, err)
	second, err := ParseMetadata([]byte(`configurations:
  - declarative_name: general.http.foo_bar
`), "/b/metadata.yaml")
	require.NoError(t, err)
	_, err = ResolveMetadata([]Metadata{first, second}, map[string]Entry{})
	require.ErrorContains(t, err, "environment variable OTEL_INSTRUMENTATION_HTTP_FOO_BAR collides")
}

func TestEnvironmentVariableName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{
			name: "general.http.client.request_captured_headers",
			want: "OTEL_INSTRUMENTATION_HTTP_CLIENT_REQUEST_CAPTURED_HEADERS",
		},
		{
			name: "go.net_http.enabled",
			want: "OTEL_INSTRUMENTATION_GO_NET_HTTP_ENABLED",
		},
		{
			name: "general.http.client.request_captured_headers/development",
			want: "OTEL_INSTRUMENTATION_HTTP_CLIENT_REQUEST_CAPTURED_HEADERS",
		},
		{
			name: "java.common.http.client.emit_experimental_telemetry/development",
			want: "OTEL_INSTRUMENTATION_JAVA_COMMON_HTTP_CLIENT_EMIT_EXPERIMENTAL_TELEMETRY",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EnvironmentVariableName(tt.name)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func indentYAML(input, prefix string) string {
	lines := strings.Split(input, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
