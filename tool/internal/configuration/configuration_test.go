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

func TestParseMetadataDeprecationFields(t *testing.T) {
	metadata, err := ParseMetadata([]byte(`configurations:
  - name: go.http.client.rules
    type: structured_list
    schema:
      type: object
      properties:
        pattern:
          type: string
    deprecated: true
    replaced_by: go.http.client.rules.v2
  - name: general.http.client.request_captured_headers/v1
    type: list
    deprecated: true
`), "metadata.yaml")
	require.NoError(t, err)
	require.Len(t, metadata.Entries, 2)
	assert.True(t, metadata.Entries[0].Deprecated)
	require.NotNil(t, metadata.Entries[0].ReplacedBy)
	assert.Equal(t, "go.http.client.rules.v2", *metadata.Entries[0].ReplacedBy)
	assert.True(t, metadata.Entries[1].Deprecated)
	assert.Nil(t, metadata.Entries[1].ReplacedBy)
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
		{
			"deprecated must be boolean",
			"name: go.x\ntype: string\ndeprecated: \"true\"",
			"deprecated: expected a boolean",
		},
		{
			"replacement requires deprecation",
			"name: go.x\ntype: string\nreplaced_by: go.y",
			"replaced_by requires deprecated to be true",
		},
		{
			"empty replacement",
			"name: go.x\ntype: string\ndeprecated: true\nreplaced_by: \"\"",
			"replaced_by must not be empty",
		},
		{
			"replacement whitespace",
			"name: go.x\ntype: string\ndeprecated: true\nreplaced_by: \" go.y \"",
			"replaced_by must not have leading or trailing whitespace",
		},
		{
			"replacement must be canonical",
			"name: go.x\ntype: string\ndeprecated: true\nreplaced_by: other.y",
			"replaced_by must be a canonical general. or go. configuration path",
		},
		{
			"reference cannot override deprecation",
			"ref: general.x\ndeprecated: true",
			"a ref entry must not include inline configuration fields",
		},
		{
			"general name must be canonical",
			"name: general.x/development\ntype: boolean",
			"name must be a canonical general. or go. configuration path",
		},
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
			"bad deprecation value",
			"name: go.test\ntype: boolean\ndeprecated: \"true\"",
			"deprecated: expected a boolean",
		},
		{
			"replacement requires deprecation",
			"name: go.test\ntype: boolean\nreplaced_by: go.test.v2",
			"replaced_by requires deprecated to be true",
		},
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

	replacement := "go.test.v2"
	definitions, err = ParseCatalog(catalogFixture(
		"name: go.test\ntype: boolean\ndeprecated: true\nreplaced_by: " + replacement,
	))
	require.NoError(t, err)
	assert.True(t, definitions["go.test"].Deprecated)
	require.NotNil(t, definitions["go.test"].ReplacedBy)
	assert.Equal(t, replacement, *definitions["go.test"].ReplacedBy)
}

func catalogFixture(definition string) []byte {
	return []byte(
		"catalog_version: 1\nsource:\n  repository: " + OpenTelemetryConfigurationRepository + "\n  release: " + OpenTelemetryConfigurationRelease +
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

func TestResolveMetadataPreservesDeprecationFromCatalog(t *testing.T) {
	replacement := "general.http.client.request_captured_headers"
	definition := Entry{
		Name:       "general.http.client.legacy_request_headers",
		Type:       "list",
		Deprecated: true,
		ReplacedBy: &replacement,
	}
	metadata, err := ParseMetadata(
		[]byte("configurations:\n  - ref: general.http.client.legacy_request_headers\n"),
		"metadata.yaml",
	)
	require.NoError(t, err)
	resolved, err := ResolveMetadata(
		[]Metadata{metadata, metadata},
		map[string]Entry{definition.Name: definition},
	)
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.True(t, resolved[0].Deprecated)
	require.NotNil(t, resolved[0].ReplacedBy)
	assert.Equal(t, replacement, *resolved[0].ReplacedBy)

	current, err := ParseMetadata(
		[]byte("configurations:\n  - name: general.http.client.legacy_request_headers\n    type: list\n"),
		"current.yaml",
	)
	require.NoError(t, err)
	_, err = ResolveMetadata([]Metadata{metadata, current}, map[string]Entry{definition.Name: definition})
	require.ErrorContains(t, err, "conflicting configuration definitions")
}

func TestGenerateCatalogRetainsDeprecatedGeneralCompatibilityDefinitions(t *testing.T) {
	replacement := "general.http.client.request_captured_headers"
	const upstream = `properties:
  general:
    properties:
      http:
        properties:
          client:
            properties:
              request_captured_headers:
                type: array
                description: Current upstream header option.
                defaultBehavior: no outbound request headers are captured
                items: {type: string}
`
	compatibility := []byte(`configurations:
  - name: general.http.client.legacy_request_headers
    description: The previous outbound request header option.
    type: list
    deprecated: true
    replaced_by: general.http.client.request_captured_headers
  - name: general.http.client.request_captured_headers
    type: list
    deprecated: true
`)
	previous := catalogDocument{
		CatalogVersion: 1,
		Source: catalogSource{
			Repository: OpenTelemetryConfigurationRepository,
			Release:    OpenTelemetryConfigurationRelease,
			Commit:     OpenTelemetryConfigurationCommit,
			File:       OpenTelemetryConfigurationFile,
		},
		Definitions: map[string]Entry{
			"general.http.client.legacy_request_headers": {
				Name:        "general.http.client.legacy_request_headers",
				Type:        listType,
				Description: "The previous outbound request header option.",
				Deprecated:  true,
				ReplacedBy:  &replacement,
			},
			"general.http.client.request_captured_headers": {
				Name:            "general.http.client.request_captured_headers",
				Type:            listType,
				Description:     "Prior upstream description.",
				DefaultBehavior: "Prior default behavior.",
			},
		},
	}
	previousData, err := yaml.Marshal(previous)
	require.NoError(t, err)
	generated, err := generateCatalog([]byte(upstream), []byte("configurations: []\n"), compatibility, previousData)
	require.NoError(t, err)
	definitions, err := ParseCatalog(generated)
	require.NoError(t, err)
	entry, exists := definitions["general.http.client.legacy_request_headers"]
	require.True(t, exists, "compatibility entry must survive when it is absent from the pinned schema")
	assert.True(t, entry.Deprecated)
	require.NotNil(t, entry.ReplacedBy)
	assert.Equal(t, replacement, *entry.ReplacedBy)
	upstreamEntry := definitions["general.http.client.request_captured_headers"]
	assert.True(t, upstreamEntry.Deprecated)
	assert.Equal(t, "Current upstream header option.", upstreamEntry.Description)
	assert.Equal(t, "no outbound request headers are captured", upstreamEntry.DefaultBehavior)
}

func TestGenerateCatalogRejectsDroppedDefinitionWithoutCompatibilityEntry(t *testing.T) {
	const upstream = `properties:
  general:
    properties:
      http:
        properties:
          client:
            properties:
              request_captured_headers:
                type: array
                items: {type: string}
`
	previous := catalogDocument{
		CatalogVersion: 1,
		Source: catalogSource{
			Repository: OpenTelemetryConfigurationRepository,
			Release:    OpenTelemetryConfigurationRelease,
			Commit:     OpenTelemetryConfigurationCommit,
			File:       OpenTelemetryConfigurationFile,
		},
		Definitions: map[string]Entry{
			"general.http.client.legacy_request_headers": {
				Name: "general.http.client.legacy_request_headers",
				Type: listType,
			},
		},
	}
	previousData, err := yaml.Marshal(previous)
	require.NoError(t, err)
	_, err = generateCatalog(
		[]byte(upstream),
		[]byte("configurations: []\n"),
		[]byte("configurations: []\n"),
		previousData,
	)
	require.ErrorContains(t, err, "catalog generation would drop \"general.http.client.legacy_request_headers\"")
}

func TestGenerateCatalogDoesNotSilentlyDropPriorDeprecationMetadata(t *testing.T) {
	const upstream = `properties:
  general:
    properties:
      http:
        properties:
          client:
            properties:
              request_captured_headers:
                type: array
                items: {type: string}
`
	replacement := "general.http.client.request_captured_headers.v2"
	previous := catalogDocument{
		CatalogVersion: 1,
		Source: catalogSource{
			Repository: OpenTelemetryConfigurationRepository,
			Release:    OpenTelemetryConfigurationRelease,
			Commit:     OpenTelemetryConfigurationCommit,
			File:       OpenTelemetryConfigurationFile,
		},
		Definitions: map[string]Entry{
			"general.http.client.request_captured_headers": {
				Name:       "general.http.client.request_captured_headers",
				Type:       listType,
				Deprecated: true,
				ReplacedBy: &replacement,
			},
		},
	}
	previousData, err := yaml.Marshal(previous)
	require.NoError(t, err)
	compatibility := []byte(`configurations:
  - name: general.http.client.request_captured_headers
    type: list
    deprecated: true
`)
	_, err = generateCatalog([]byte(upstream), []byte("configurations: []\n"), compatibility, previousData)
	require.ErrorContains(t, err, "catalog generation would clear replaced_by")
}

func TestGenerateCatalogDoesNotSilentlyClearDeprecatedFlag(t *testing.T) {
	const upstream = `properties:
  general:
    properties:
      http:
        properties:
          client:
            properties:
              request_captured_headers:
                type: array
                items: {type: string}
`
	previous := catalogDocument{
		CatalogVersion: 1,
		Source: catalogSource{
			Repository: OpenTelemetryConfigurationRepository,
			Release:    OpenTelemetryConfigurationRelease,
			Commit:     OpenTelemetryConfigurationCommit,
			File:       OpenTelemetryConfigurationFile,
		},
		Definitions: map[string]Entry{
			"general.http.client.request_captured_headers": {
				Name:       "general.http.client.request_captured_headers",
				Type:       listType,
				Deprecated: true,
			},
		},
	}
	previousData, err := yaml.Marshal(previous)
	require.NoError(t, err)
	_, err = generateCatalog(
		[]byte(upstream),
		[]byte("configurations: []\n"),
		[]byte("configurations: []\n"),
		previousData,
	)
	require.ErrorContains(t, err, "catalog generation would clear deprecation")
}

func TestGenerateCatalogIncludesDeprecatedInlineGoDefinitions(t *testing.T) {
	goOptions := []byte(`configurations:
  - name: go.http.client.legacy_rules
    type: structured_list
    schema: {type: object, properties: {pattern: {type: string}}}
    deprecated: true
    replaced_by: go.http.client.rules
  - name: go.http.client.rules
    type: structured_list
    schema: {type: object, properties: {pattern: {type: string}}}
`)
	generated, err := generateCatalog(
		upstreamInstrumentation,
		goOptions,
		[]byte("configurations: []\n"),
		embeddedCatalog,
	)
	require.NoError(t, err)
	definitions, err := ParseCatalog(generated)
	require.NoError(t, err)
	legacy := definitions["go.http.client.legacy_rules"]
	assert.True(t, legacy.Deprecated)
	require.NotNil(t, legacy.ReplacedBy)
	assert.Equal(t, "go.http.client.rules", *legacy.ReplacedBy)
}

func TestApplyGeneralCompatibilityOverridesRejectsInvalidEntries(t *testing.T) {
	tests := []struct{ name, metadata, want string }{
		{
			name:     "Go namespace",
			metadata: "configurations:\n  - name: go.old\n    type: string\n    deprecated: true\n",
			want:     "must name a general.* path directly",
		},
		{
			name:     "not deprecated",
			metadata: "configurations:\n  - name: general.old\n    type: string\n",
			want:     "must be deprecated",
		},
		{
			name: "duplicate path",
			metadata: "configurations:\n" +
				"  - name: general.old\n    type: string\n    deprecated: true\n" +
				"  - name: general.old\n    type: string\n    deprecated: true\n",
			want: "duplicate compatibility definition",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := applyGeneralCompatibilityOverrides(make(map[string]Entry), []byte(tt.metadata))
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestGeneralCompatibilityRejectsChangingLiveUpstreamType(t *testing.T) {
	definitions := map[string]Entry{
		"general.http.client.request_captured_headers": {
			Name: "general.http.client.request_captured_headers",
			Type: listType,
		},
	}
	err := applyGeneralCompatibilityOverrides(definitions, []byte(`configurations:
  - name: general.http.client.request_captured_headers
    type: boolean
    deprecated: true
`))
	require.ErrorContains(t, err, "does not match current upstream type")
}

func TestEnsureCatalogDefinitionsRetainedRejectsClearingDeprecatedFlag(t *testing.T) {
	previous := map[string]Entry{
		"general.http.client.request_captured_headers": {
			Name:       "general.http.client.request_captured_headers",
			Type:       listType,
			Deprecated: true,
		},
	}
	current := map[string]Entry{
		"general.http.client.request_captured_headers": {
			Name: "general.http.client.request_captured_headers",
			Type: listType,
		},
	}
	require.ErrorContains(t, ensureCatalogDefinitionsRetained(previous, current), "would clear deprecation")
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

func TestParseMetadataRejectsMalformedDocuments(t *testing.T) {
	tests := []struct{ name, input, want string }{
		{"invalid YAML", "configurations: [", "parse metadata.yaml"},
		{"non-mapping document", "[]", "metadata document must be a mapping"},
		{"duplicate root key", "configurations: []\nconfigurations: []\n", "duplicate key"},
		{"configurations is not a sequence", "configurations: {}", "configurations must be a sequence"},
		{"entry is not a mapping", "configurations: [null]", "expected a mapping"},
		{
			"examples is not a sequence",
			"configurations: [{name: go.x, type: string, examples: example}]",
			"examples: expected a sequence of strings",
		},
		{
			"example is not a string",
			"configurations: [{name: go.x, type: string, examples: [1]}]",
			"examples[0]: expected a string",
		},
		{
			"default behavior is not a string",
			"configurations: [{name: go.x, type: string, default_behavior: true}]",
			"default_behavior: expected a string",
		},
		{
			"replacement is not a string",
			"configurations: [{name: go.x, type: string, deprecated: true, replaced_by: true}]",
			"replaced_by: expected a string",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseMetadata([]byte(tt.input), "metadata.yaml")
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestParseMetadataRejectsMalformedSchemas(t *testing.T) {
	tests := []struct{ name, schema, want string }{
		{"schema is not a mapping", "[]", "schema: expected a mapping"},
		{"schema type missing", "{properties: {}}", "schema: type is required"},
		{"schema type is not a string", "{type: true, properties: {}}", "schema.type: expected a string"},
		{"properties missing", "{type: object}", "schema: properties is required"},
		{"properties is not a mapping", "{type: object, properties: []}", "schema.properties: expected a mapping"},
		{
			"required is not a sequence",
			"{type: object, required: name, properties: {}}",
			"schema.required: expected a sequence",
		},
		{
			"required member is not a string",
			"{type: object, required: [1], properties: {}}",
			"schema.required[0]: expected a string",
		},
		{
			"duplicate required member",
			"{type: object, required: [name, name], properties: {name: {type: string}}}",
			"schema.required: duplicate key",
		},
		{
			"empty property name",
			"{type: object, properties: {\"\": {type: string}}}",
			"schema.properties: property name must not be empty",
		},
		{
			"property is not a mapping",
			"{type: object, properties: {name: []}}",
			"schema.properties.name: expected a mapping",
		},
		{
			"property type missing",
			"{type: object, properties: {name: {description: a name}}}",
			"schema.properties.name: type is required",
		},
		{
			"property description is not a string",
			"{type: object, properties: {name: {type: string, description: true}}}",
			"schema.properties.name.description: expected a string",
		},
		{
			"unsupported property type",
			"{type: object, properties: {name: {type: date}}}",
			"unsupported property type \"date\"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := "configurations:\n  - name: go.x\n    type: structured_list\n    schema: " + tt.schema + "\n"
			_, err := ParseMetadata([]byte(input), "metadata.yaml")
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestParseCatalogRejectsMalformedDocuments(t *testing.T) {
	valid := string(catalogFixture("name: go.test\ntype: string"))
	sourceFields := fmt.Sprintf(
		"source:\n  repository: %s\n  release: %s\n  commit: %s\n  file: %s\n",
		OpenTelemetryConfigurationRepository,
		OpenTelemetryConfigurationRelease,
		OpenTelemetryConfigurationCommit,
		OpenTelemetryConfigurationFile,
	)
	definitions := "definitions:\n  go.test:\n    name: go.test\n    type: string\n"
	withoutDefinitions := strings.TrimSuffix(valid, definitions)
	tests := []struct{ name, input, want string }{
		{"invalid YAML", "catalog_version: [", "parse configuration catalog"},
		{"multiple documents", valid + "---\nother: document\n", "single YAML document"},
		{"malformed trailing document", valid + "---\nother: [\n", "parse trailing configuration catalog document"},
		{"non-mapping root", "[]", "expected a mapping"},
		{
			"unknown root field",
			strings.Replace(valid, "catalog_version:", "unknown: value\ncatalog_version:", 1),
			"unknown field",
		},
		{"unsupported version", strings.Replace(valid, "catalog_version: 1", "catalog_version: 2", 1), "expected 1"},
		{"source missing", strings.Replace(valid, sourceFields, "", 1), "source is required"},
		{
			"unknown source field",
			strings.Replace(valid, "  release:", "  unexpected: value\n  release:", 1),
			"unknown field",
		},
		{
			"source pin mismatch",
			strings.Replace(valid, OpenTelemetryConfigurationCommit, "wrong-commit", 1),
			"must pin",
		},
		{"definitions missing", withoutDefinitions, "has no definitions"},
		{
			"definitions is not a mapping",
			strings.Replace(valid, definitions, "definitions: []\n", 1),
			"expected a mapping",
		},
		{"definitions are empty", strings.Replace(valid, definitions, "definitions: {}\n", 1), "has no definitions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCatalog([]byte(tt.input))
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestParsePreviousCatalogDefinitionsRejectsInvalidCatalogs(t *testing.T) {
	base := catalogDocument{
		CatalogVersion: 1,
		Source: catalogSource{
			Repository: OpenTelemetryConfigurationRepository,
			Release:    OpenTelemetryConfigurationRelease,
			Commit:     OpenTelemetryConfigurationCommit,
			File:       OpenTelemetryConfigurationFile,
		},
		Definitions: map[string]Entry{
			"general.http.legacy": {Name: "general.http.legacy", Type: stringType},
		},
	}
	valid, err := yaml.Marshal(base)
	require.NoError(t, err)
	unknownField := strings.Replace(string(valid), "catalog_version: 1", "unknown: value\ncatalog_version: 1", 1)
	unsupportedVersion := strings.Replace(string(valid), "catalog_version: 1", "catalog_version: 2", 1)
	wrongRepository := strings.Replace(string(valid), OpenTelemetryConfigurationRepository, "other/repository", 1)
	emptyDefinitions, err := yaml.Marshal(catalogDocument{
		CatalogVersion: 1,
		Source:         base.Source,
		Definitions:    map[string]Entry{},
	})
	require.NoError(t, err)
	withoutDefinitions := fmt.Sprintf(
		"catalog_version: 1\nsource:\n  repository: %s\n  release: %s\n  commit: %s\n  file: %s\n",
		OpenTelemetryConfigurationRepository,
		OpenTelemetryConfigurationRelease,
		OpenTelemetryConfigurationCommit,
		OpenTelemetryConfigurationFile,
	)
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{"invalid YAML", []byte("catalog_version: ["), "parse previous configuration catalog"},
		{"unknown field", []byte(unknownField), "field unknown not found"},
		{"unsupported version", []byte(unsupportedVersion), "unsupported previous configuration catalog version"},
		{"wrong repository", []byte(wrongRepository), "must identify a pinned"},
		{"missing definitions", []byte(withoutDefinitions), "no definitions"},
		{"empty definitions", emptyDefinitions, "no definitions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, parseErr := parsePreviousCatalogDefinitions(tt.input)
			require.ErrorContains(t, parseErr, tt.want)
		})
	}
}

func TestGenerateCatalogRejectsInvalidSources(t *testing.T) {
	const upstream = `properties:
  general:
    properties:
      http:
        properties:
          client:
            properties:
              request_captured_headers:
                type: array
                items: {type: string}
`
	tests := []struct {
		name                               string
		upstream, goOptions, compatibility []byte
		want                               string
	}{
		{
			"invalid upstream YAML",
			[]byte("properties: ["),
			[]byte("configurations: []"),
			[]byte("configurations: []"),
			"parse pinned instrumentation schema",
		},
		{
			"general section missing",
			[]byte("properties: {}"),
			[]byte("configurations: []"),
			[]byte("configurations: []"),
			"has no general property",
		},
		{
			"invalid compatibility YAML",
			[]byte(upstream),
			[]byte("configurations: []"),
			[]byte("configurations: ["),
			"parse general-compatibility.yaml",
		},
		{
			"invalid Go metadata",
			[]byte(upstream),
			[]byte("configurations: ["),
			[]byte("configurations: []"),
			"parse go-configurations.yaml",
		},
		{
			"Go metadata uses a general path",
			[]byte(upstream),
			[]byte("configurations:\n  - name: general.http.client.request_captured_headers\n    type: list\n"),
			[]byte("configurations: []"),
			"must name a go.* path directly",
		},
		{
			"Go metadata uses a ref",
			[]byte(upstream),
			[]byte("configurations:\n  - ref: general.http.client.request_captured_headers\n"),
			[]byte("configurations: []"),
			"must name a go.* path directly",
		},
		{
			"duplicate Go path",
			[]byte(upstream),
			[]byte(
				"configurations:\n  - name: go.http.client.rules\n    type: string\n  - name: go.http.client.rules\n    type: string\n",
			),
			[]byte("configurations: []"),
			"duplicate catalog name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := generateCatalog(tt.upstream, tt.goOptions, tt.compatibility, embeddedCatalog)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestCollectGeneralDefinitionsRejectsCyclicReference(t *testing.T) {
	node := &upstreamSchema{Ref: "#/$defs/Loop"}
	definitions := map[string]*upstreamSchema{"Loop": {Ref: "#/$defs/Loop"}}

	err := collectGeneralDefinitions(node, "general.loop", definitions, make(map[string]bool), make(map[string]Entry))

	require.ErrorContains(t, err, "unsupported or cyclic schema reference")
}

func TestUpstreamTypeRejectsNullOnlyUnion(t *testing.T) {
	var schema upstreamSchema
	require.NoError(t, yaml.Unmarshal([]byte(`type: ["null"]`), &schema))

	_, err := upstreamType(schema.Type)

	require.ErrorContains(t, err, "must not be null-only")
}

func TestResolveMetadataClonesCatalogEntries(t *testing.T) {
	defaultValue := "enabled"
	replacement := "go.http.client.rules.v2"
	definition := Entry{
		Name:       "go.http.client.rules",
		Type:       structuredListType,
		Default:    &defaultValue,
		Examples:   []string{"first"},
		Deprecated: true,
		ReplacedBy: &replacement,
		Schema: &Schema{
			Type:       "object",
			Required:   []string{"name"},
			Properties: map[string]Property{"name": {Type: stringType}},
		},
	}
	metadata := Metadata{Entries: []Entry{{Ref: definition.Name, Source: "metadata.yaml"}}}

	resolved, err := ResolveMetadata([]Metadata{metadata}, map[string]Entry{definition.Name: definition})
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	*resolved[0].Default = "changed"
	resolved[0].Examples[0] = "changed"
	*resolved[0].ReplacedBy = "changed"
	resolved[0].Schema.Required[0] = "changed"
	resolved[0].Schema.Properties["extra"] = Property{Type: stringType}

	assert.Equal(t, "enabled", *definition.Default)
	assert.Equal(t, []string{"first"}, definition.Examples)
	assert.Equal(t, "go.http.client.rules.v2", *definition.ReplacedBy)
	assert.Equal(t, []string{"name"}, definition.Schema.Required)
	assert.NotContains(t, definition.Schema.Properties, "extra")
}

func TestEnvironmentVariableNameRejectsInvalidPaths(t *testing.T) {
	for _, tt := range []struct{ path, want string }{
		{"", "non-empty"},
		{" go.option ", "surrounding whitespace"},
		{"general.", "no environment-variable path"},
		{"go.\u00e9", "unsupported character"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			_, err := EnvironmentVariableName(tt.path)
			require.ErrorContains(t, err, tt.want)
		})
	}
}
