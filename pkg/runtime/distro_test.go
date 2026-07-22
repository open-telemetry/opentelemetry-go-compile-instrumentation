// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

func TestDistroResource(t *testing.T) {
	res := distroResource()
	require.NotNil(t, res)

	attrs := res.Attributes()
	attrMap := make(map[attribute.Key]attribute.Value, len(attrs))
	for _, a := range attrs {
		attrMap[a.Key] = a.Value
	}

	t.Run("telemetry.distro.name is set", func(t *testing.T) {
		val, ok := attrMap[attribute.Key("telemetry.distro.name")]
		require.True(t, ok, "telemetry.distro.name must be present")
		assert.Equal(t, distroName, val.AsString())
	})

	t.Run("telemetry.distro.version reflects OtelcVersion", func(t *testing.T) {
		val, ok := attrMap[attribute.Key("telemetry.distro.version")]
		require.True(t, ok, "telemetry.distro.version must be present")
		assert.Equal(t, OtelcVersion, val.AsString())
	})
}

func TestDistroResourceVersionOverride(t *testing.T) {
	orig := OtelcVersion
	t.Cleanup(func() { OtelcVersion = orig })

	OtelcVersion = "v1.2.3"
	res := distroResource()

	for _, a := range res.Attributes() {
		if a.Key == attribute.Key("telemetry.distro.version") {
			assert.Equal(t, "v1.2.3", a.Value.AsString())
			return
		}
	}
	t.Fatal("telemetry.distro.version attribute not found")
}

func TestDistroResourceDefaultSentinel(t *testing.T) {
	// The sentinel value for non-instrumented builds should be "dev".
	orig := OtelcVersion
	t.Cleanup(func() { OtelcVersion = orig })

	OtelcVersion = "dev"
	res := distroResource()

	for _, a := range res.Attributes() {
		if a.Key == attribute.Key("telemetry.distro.version") {
			assert.Equal(t, "dev", a.Value.AsString(),
				"OtelcVersion sentinel should be 'dev' for plain builds")
			return
		}
	}
	t.Fatal("telemetry.distro.version attribute not found")
}

func TestDistroResourceMergeEnvWins(t *testing.T) {
	// Simulate what setupOpenTelemetry does: merge distro at lowest priority.
	// The env-sourced resource (r2) must win over the distro resource (r1).
	override := resource.NewWithAttributes(
		resource.Default().SchemaURL(),
		attribute.String("telemetry.distro.version", "custom-override"),
	)

	distro := distroResource()
	merged, err := resource.Merge(distro, override)
	require.NoError(t, err)

	for _, a := range merged.Attributes() {
		if a.Key == attribute.Key("telemetry.distro.version") {
			assert.Equal(t, "custom-override", a.Value.AsString(),
				"user-supplied attribute must override the baked-in distro version")
			return
		}
	}
	t.Fatal("telemetry.distro.version attribute not found after merge")
}
