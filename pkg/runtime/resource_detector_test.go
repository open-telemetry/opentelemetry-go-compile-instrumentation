// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

// detectorFunc adapts a function to resource.Detector.
type detectorFunc func(context.Context) (*resource.Resource, error)

func (f detectorFunc) Detect(ctx context.Context) (*resource.Resource, error) { return f(ctx) }

// staticDetector returns a detector that always detects attrs.
func staticDetector(attrs ...attribute.KeyValue) resource.Detector {
	return detectorFunc(func(context.Context) (*resource.Resource, error) {
		return resource.NewSchemaless(attrs...), nil
	})
}

// resetResourceDetectors empties the registry for the test and restores it afterwards.
func resetResourceDetectors(t *testing.T) {
	t.Helper()
	swap := func(sealed bool, detectors []guardedDetector) (bool, []guardedDetector) {
		resourceDetectors.Lock()
		defer resourceDetectors.Unlock()
		origSealed, origDetectors := resourceDetectors.sealed, resourceDetectors.detectors
		resourceDetectors.sealed, resourceDetectors.detectors = sealed, detectors
		return origSealed, origDetectors
	}
	origSealed, origDetectors := swap(false, nil)
	t.Cleanup(func() { swap(origSealed, origDetectors) })
}

// captureLogs sends the package logger to a buffer for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	origLogger := logger
	t.Cleanup(func() { logger = origLogger })
	logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	return &buf
}

func resourceValue(t *testing.T, res *resource.Resource, key string) string {
	t.Helper()
	v, ok := res.Set().Value(attribute.Key(key))
	require.True(t, ok, "resource must set %s", key)
	return v.AsString()
}

func TestRegisterResourceDetectorRejectsInvalidInput(t *testing.T) {
	resetResourceDetectors(t)
	captureLogs(t)

	assert.False(t, RegisterResourceDetector("", staticDetector()), "empty name")
	assert.False(t, RegisterResourceDetector("nil", nil), "nil detector")

	assert.True(t, RegisterResourceDetector("dup", staticDetector()))
	assert.False(t, RegisterResourceDetector("dup", staticDetector()), "duplicate name")
}

func TestRegisterResourceDetectorAfterResourceIsBuilt(t *testing.T) {
	resetResourceDetectors(t)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	logs := captureLogs(t)

	newResource(context.Background())

	assert.False(t, RegisterResourceDetector("late", staticDetector()))
	assert.Contains(t, logs.String(), "registered after the resource was built")
}

func TestNewResourceMergeOrder(t *testing.T) {
	resetResourceDetectors(t)
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "faas.version=from-env")

	require.True(t, RegisterResourceDetector("first", staticDetector(
		attribute.String("service.name", "from-detector"),
		attribute.String("cloud.region", "from-first"),
		attribute.String("faas.name", "from-first"),
		attribute.String("faas.version", "from-first"),
	)))
	require.True(t, RegisterResourceDetector("second", staticDetector(
		attribute.String("faas.name", "from-second"),
	)))

	res := newResource(context.Background())

	assert.Equal(t, "from-detector", resourceValue(t, res, "service.name"),
		"a registered detector overrides the built-in detectors")
	assert.Equal(t, "from-first", resourceValue(t, res, "cloud.region"))
	assert.Equal(t, "from-second", resourceValue(t, res, "faas.name"),
		"a later registered detector overrides an earlier one")
	assert.Equal(t, "from-env", resourceValue(t, res, "faas.version"),
		"OTEL_RESOURCE_ATTRIBUTES overrides the registered detectors")
}

func TestNewResourceSkipsFailingDetectors(t *testing.T) {
	resetResourceDetectors(t)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	logs := captureLogs(t)

	require.True(t, RegisterResourceDetector("failing", detectorFunc(
		func(context.Context) (*resource.Resource, error) {
			return resource.NewSchemaless(attribute.String("cloud.region", "dropped")), errors.New("boom")
		},
	)))
	require.True(t, RegisterResourceDetector("panicking", detectorFunc(
		func(context.Context) (*resource.Resource, error) { panic("kaboom") },
	)))
	require.True(t, RegisterResourceDetector("partial", detectorFunc(
		func(context.Context) (*resource.Resource, error) {
			return resource.NewSchemaless(attribute.String("faas.name", "kept")), resource.ErrPartialResource
		},
	)))
	require.True(t, RegisterResourceDetector("healthy", staticDetector(
		attribute.String("cloud.provider", "aws"),
	)))

	res := newResource(context.Background())
	set := res.Set()

	_, ok := set.Value(attribute.Key("cloud.region"))
	assert.False(t, ok, "the resource of a detector that failed must not be merged")
	assert.Equal(t, "kept", resourceValue(t, res, "faas.name"), "a partial resource is merged")
	assert.Equal(t, "aws", resourceValue(t, res, "cloud.provider"), "later detectors still run")
	_, ok = set.Value(attribute.Key("process.pid"))
	assert.True(t, ok, "the built-in attributes are kept")

	assert.Contains(t, logs.String(), `resource detector \"failing\": boom`)
	assert.Contains(t, logs.String(), `resource detector \"panicking\": panic: kaboom`)
	assert.Contains(t, logs.String(), `resource detector \"partial\": partial resource`)
}

func TestNewResourceKeepsAttributesOnSchemaURLConflict(t *testing.T) {
	resetResourceDetectors(t)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	logs := captureLogs(t)

	// The built-in detectors use the schema URL of the SDK. A detector built
	// against another semantic conventions version conflicts with it.
	require.True(t, RegisterResourceDetector("other-schema", detectorFunc(
		func(context.Context) (*resource.Resource, error) {
			return resource.NewWithAttributes(
				"https://opentelemetry.io/schemas/1.0.0",
				attribute.String("faas.name", "my-function"),
			), nil
		},
	)))

	res := newResource(context.Background())

	assert.Equal(t, "my-function", resourceValue(t, res, "faas.name"))
	_, ok := res.Set().Value(attribute.Key("process.pid"))
	assert.True(t, ok, "the built-in attributes are kept")
	assert.Empty(t, res.SchemaURL(), "a schema URL conflict clears the schema URL")
	assert.Contains(t, logs.String(), "conflicting Schema URL")
}
