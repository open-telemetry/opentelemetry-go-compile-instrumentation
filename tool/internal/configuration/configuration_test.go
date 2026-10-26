// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestParseMetadata(t *testing.T) {
	metadata, err := ParseMetadata([]byte(`name: test-instrumentation
configurations:
  - name: go.net_http.enabled
    type: boolean
    default: true
  - ref: general.http.client.request_captured_headers
  - name: go.net_http.limit
    type: int
    default: 17
  - name: go.net_http.label
    type: string
    default: ""
  - name: go.net_http.no_default
    type: string
`), "/module/metadata.yaml")
	require.NoError(t, err)
	require.Len(t, metadata.Entries, 5)
	assert.Equal(t, "go.net_http.enabled", metadata.Entries[0].Name)
	assert.Equal(t, "boolean", metadata.Entries[0].Type)
	require.NotNil(t, metadata.Entries[0].Default)
	assert.Equal(t, "true", *metadata.Entries[0].Default)
	assert.Equal(t, "general.http.client.request_captured_headers", metadata.Entries[1].Ref)
	require.NotNil(t, metadata.Entries[2].Default)
	assert.Equal(t, "17", *metadata.Entries[2].Default)
	require.NotNil(t, metadata.Entries[3].Default)
	assert.Empty(t, *metadata.Entries[3].Default)
	assert.Nil(t, metadata.Entries[4].Default)
	assert.Equal(t, "/module/metadata.yaml:configurations[0]", metadata.Entries[0].Source)
}

func TestParseMetadataRejectsMalformedEntries(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"both selectors", "name: go.x\nref: general.x", "exactly one of name or ref"},
		{"neither selector", "description: missing", "exactly one of name or ref"},
		{"empty name", `name: ""`, "name must not be empty"},
		{"null name", "name: null", "expected a string"},
		{"whitespace name", `name: " go.x "`, "leading or trailing whitespace"},
		{"empty ref", `ref: ""`, "ref must not be empty"},
		{"null ref", "ref: null", "expected a string"},
		{"whitespace ref", `ref: " general.x "`, "ref must not have leading or trailing whitespace"},
		{"ref override", "ref: general.x\ndescription: override", "must not include inline"},
		{"empty ref override", "ref: general.x\ndescription: \"\"", "must not include inline"},
		{"null ref override", "ref: general.x\ndefault: null", "must not include inline"},
		{"missing type", "name: go.x", "unsupported or missing type"},
		{"invalid type", "name: go.x\ntype: bool", "unsupported or missing type"},
		{"old name", "declarative_name: go.x\ntype: boolean", "unknown field"},
		{"old type", "name: go.x\ndeclarative_type: boolean", "unknown field"},
		{"old schema", "name: go.x\ndeclarative_schema: {}", "unknown field"},
		{"duplicate name", "name: go.x\nname: go.y\ntype: string", "duplicate key"},
		{"invalid default", "name: go.x\ntype: boolean\ndefault: yes", "not a valid boolean"},
		{"invalid integer", "name: go.x\ntype: int\ndefault: 2.5", "not a valid integer"},
		{"null default", "name: go.x\ntype: string\ndefault: null", "default must be"},
		{"non-scalar default", "name: go.x\ntype: list\ndefault: []", "default must be"},
		{"wrong list default", "name: go.x\ntype: list\ndefault: nope", "default must encode"},
		{"wrong map default", "name: go.x\ntype: map\ndefault: \"[]\"", "default must encode"},
		{
			"missing required default property",
			"name: go.x\ntype: structured_list\ndefault: \"[{}]\"\nschema: {type: object, required: [enabled], properties: {enabled: {type: boolean}}}",
			"required schema property",
		},
		{
			"wrong typed default property",
			"name: go.x\ntype: structured_list\ndefault: \"[{enabled: nope}]\"\nschema: {type: object, properties: {enabled: {type: boolean}}}",
			"value does not match type",
		},
		{
			"schema on scalar",
			"name: go.x\ntype: string\nschema: {type: object, properties: {}}",
			"schema requires type structured_list",
		},
		{"missing schema", "name: go.x\ntype: structured_list", "structured_list requires schema"},
		{
			"wrong schema root",
			"name: go.x\ntype: structured_list\nschema: {type: string, properties: {}}",
			"type must be object",
		},
		{
			"missing property",
			"name: go.x\ntype: structured_list\nschema: {type: object, required: [missing], properties: {}}",
			"required key",
		},
		{
			"wrong property default",
			"name: go.x\ntype: structured_list\nschema: {type: object, properties: {enabled: {type: boolean, default: \"false\"}}}",
			"default: value does not match type",
		},
		{
			"wrong property example",
			"name: go.x\ntype: structured_list\nschema: {type: object, properties: {count: {type: int, example: false}}}",
			"example: value does not match type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseMetadata(
				[]byte("configurations:\n  - "+strings.ReplaceAll(tt.body, "\n", "\n    ")+"\n"),
				"metadata.yaml",
			)
			require.ErrorContains(t, err, tt.want)
			require.ErrorContains(t, err, "metadata.yaml")
		})
	}
}

func TestParseMetadataPropertyValuesRetainYAMLTypes(t *testing.T) {
	metadata, err := ParseMetadata([]byte(`configurations:
  - name: go.http.rules
    type: structured_list
    default: "[{enabled: false, pattern: /users}]"
    schema:
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
	properties := metadata.Entries[0].Schema.Properties
	assert.Equal(t, false, properties["enabled"].Default)
	assert.Equal(t, true, properties["enabled"].Example)
	assert.Equal(t, "/users/{id}", properties["pattern"].Example)
}

func TestConfigurationCatalogMatchesPinnedSource(t *testing.T) {
	assert.Equal(t, "a6e45b6e5f6eb647f9101b5e045619b79368c83b561629e522e7c536775403bb",
		fmt.Sprintf("%x", sha256.Sum256(upstreamInstrumentation)), "snapshot must match the pinned upstream release")
	generated, err := GenerateCatalog()
	require.NoError(t, err)
	// Compare YAML values rather than formatting: repository-wide yamlfmt may
	// reflow long descriptions, but it must not change catalog content.
	var expected, actual any
	require.NoError(t, yaml.Unmarshal(generated, &expected))
	require.NoError(t, yaml.Unmarshal(embeddedCatalog, &actual))
	assert.Equal(t, expected, actual, "run go generate ./tool/internal/configuration")
	definitions, err := CatalogDefinitions()
	require.NoError(t, err)
	require.Len(t, definitions, 26)
	header := definitions["general.http.client.request_captured_headers"]
	assert.Equal(t, "list", header.Type)
	assert.Equal(t, "Configure headers to capture for outbound http requests.", header.Description)
	assert.Equal(t, "no outbound request headers are captured", header.DefaultBehavior)
	assert.Nil(t, header.Default, "prose defaultBehavior must not become an executable default")
	assert.Equal(t, "int", definitions["general.http.semconv.version"].Type)
	assert.Equal(t, "boolean", definitions["general.http.semconv.experimental"].Type)
	assert.NotContains(t, definitions, "general.http.client.request_captured_headers/development")
}

func TestParseCatalogValidatesInlineDefinitions(t *testing.T) {
	tests := []struct{ name, definition, want string }{
		{"nested ref", "ref: general.other", "must use name, not another ref"},
		{"mismatched key", "name: go.other\ntype: boolean", "key must equal name"},
		{"unknown field", "name: go.test\ntype: string\nunknown: value", "unknown field"},
		{"missing type", "name: go.test", "unsupported or missing type"},
		{"bad default", "name: go.test\ntype: boolean\ndefault: nope", "not a valid boolean"},
		{
			"bad property default",
			"name: go.test\ntype: structured_list\nschema: {type: object, properties: {enabled: {type: boolean, default: \"false\"}}}",
			"default: value does not match type",
		},
		{
			"bad property example",
			"name: go.test\ntype: structured_list\nschema: {type: object, properties: {enabled: {type: boolean, example: 0}}}",
			"example: value does not match type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCatalog(catalogFixture(tt.definition))
			require.ErrorContains(t, err, tt.want)
		})
	}
	definitions, err := ParseCatalog(catalogFixture("name: go.test\ntype: boolean\ndefault: false"))
	require.NoError(t, err)
	require.NotNil(t, definitions["go.test"].Default)
	assert.Equal(t, "false", *definitions["go.test"].Default)
	assert.Contains(t, definitions["go.test"].Source, "otelc-owned")
}

func catalogFixture(definition string) []byte {
	return []byte(
		"catalog_version: 1\nsource:\n  repository: open-telemetry/opentelemetry-configuration\n  release: " + OpenTelemetryConfigurationRelease +
			"\n  commit: " + OpenTelemetryConfigurationCommit + "\n  file: schema/instrumentation.yaml\ndefinitions:\n  go.test:\n    " +
			strings.ReplaceAll(
				definition,
				"\n",
				"\n    ",
			) + "\n",
	)
}

func TestResolveMetadata(t *testing.T) {
	definitions, err := CatalogDefinitions()
	require.NoError(t, err)
	first, err := ParseMetadata(
		[]byte("configurations:\n  - ref: general.http.client.request_captured_headers\n"),
		"/a/metadata.yaml",
	)
	require.NoError(t, err)
	second, err := ParseMetadata(
		[]byte("configurations:\n  - ref: general.http.client.request_captured_headers\n"),
		"/b/metadata.yaml",
	)
	require.NoError(t, err)
	resolved, err := ResolveMetadata([]Metadata{first, second}, definitions)
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, "general.http.client.request_captured_headers", resolved[0].Name)
	assert.Equal(t, resolved[0].Name, resolved[0].Ref)
	assert.Contains(t, resolved[0].DefinitionSource, OpenTelemetryConfigurationCommit)
	conflictingInline, err := ParseMetadata(
		[]byte("configurations:\n  - name: general.http.client.request_captured_headers\n    type: string\n"),
		"inline.yaml",
	)
	require.NoError(t, err)
	_, err = ResolveMetadata([]Metadata{first, conflictingInline}, definitions)
	require.ErrorContains(t, err, "conflicting configuration definitions")
	require.ErrorContains(t, err, "inline.yaml")
	require.ErrorContains(t, err, "/a/metadata.yaml")

	unknown, err := ParseMetadata([]byte("configurations:\n  - ref: missing\n"), "unknown.yaml")
	require.NoError(t, err)
	_, err = ResolveMetadata([]Metadata{unknown}, definitions)
	require.ErrorContains(t, err, `unknown configuration ref "missing"`)

	left, err := ParseMetadata(
		[]byte("configurations:\n  - name: go.option\n    type: string\n    default: one\n"),
		"left.yaml",
	)
	require.NoError(t, err)
	right, err := ParseMetadata(
		[]byte("configurations:\n  - name: go.option\n    type: string\n    default: two\n"),
		"right.yaml",
	)
	require.NoError(t, err)
	_, err = ResolveMetadata([]Metadata{left, right}, definitions)
	require.ErrorContains(t, err, `conflicting configuration definitions for name "go.option"`)
	require.ErrorContains(t, err, "left.yaml")
	require.ErrorContains(t, err, "right.yaml")
}

func TestGeneralCatalogGenerationRejectsUnsupportedSourceShapes(t *testing.T) {
	for _, tt := range []struct{ name, schema, want string }{
		{"unknown reference", "$ref: '#/$defs/missing'", "unknown schema reference"},
		{"external reference", "$ref: 'https://example.com/schema'", "unsupported or cyclic"},
		{"unsupported leaf", "type: number", "unsupported upstream leaf type"},
		{"untyped leaf", "description: missing type", "upstream type must be"},
		{"ambiguous union", "type: [boolean, string, 'null']", "only one non-null type"},
		{"unsupported array", "type: array\nitems: {type: object}", "array must have string items"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var source upstreamSchema
			require.NoError(t, yaml.Unmarshal([]byte(tt.schema), &source))
			err := collectGeneralDefinitions(
				&source,
				"general.test",
				nil,
				make(map[string]bool),
				make(map[string]Entry),
			)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestResolveMetadataDetectsEnvironmentNameCollisions(t *testing.T) {
	metadata, err := ParseMetadata(
		[]byte(
			"configurations:\n  - name: general.foo-bar\n    type: string\n  - name: general.foo_bar\n    type: string\n",
		),
		"metadata.yaml",
	)
	require.NoError(t, err)
	_, err = ResolveMetadata([]Metadata{metadata}, nil)
	require.ErrorContains(t, err, "environment variable OTEL_INSTRUMENTATION_FOO_BAR collides")
}

func TestEnvironmentVariableName(t *testing.T) {
	for path, want := range map[string]string{
		"general.http.client.request_captured_headers": "OTEL_INSTRUMENTATION_HTTP_CLIENT_REQUEST_CAPTURED_HEADERS",
		"go.net_http.enabled":                          "OTEL_INSTRUMENTATION_GO_NET_HTTP_ENABLED",
		"go.feature/development":                       "OTEL_INSTRUMENTATION_GO_FEATURE",
	} {
		t.Run(path, func(t *testing.T) {
			got, err := EnvironmentVariableName(path)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}
