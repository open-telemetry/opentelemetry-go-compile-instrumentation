// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package mongodb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/event"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

func TestMongoEnabler(t *testing.T) {
	tests := []struct {
		name     string
		setupEnv func(t *testing.T)
		expected bool
	}{
		{
			name: "enabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB")
			},
			expected: true,
		},
		{
			name: "disabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "MONGODB")
			},
			expected: false,
		},
		{
			name: "not in enabled list",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "REDIS")
			},
			expected: false,
		},
		{
			name: "default enabled when no env set",
			setupEnv: func(t *testing.T) {
				// No environment variables set - should be enabled by default
			},
			expected: true,
		},
		{
			name: "enabled with multiple instrumentations",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "REDIS,MONGODB,GRPC")
			},
			expected: true,
		},
		{
			name: "disabled with multiple instrumentations",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "MONGODB,GRPC")
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupEnv(t)

			result := enabler.Enable()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBeforeConnect(t *testing.T) {
	t.Run("injects monitor when opts is empty", func(t *testing.T) {
		t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB")

		mockCtx := hooktest.NewMockHookContext(t.Context())

		BeforeConnect(mockCtx, t.Context())

		newOpts, ok := mockCtx.GetParam(1).([]*options.ClientOptions)
		require.True(t, ok, "param 1 should be updated with a []*options.ClientOptions")
		require.Len(t, newOpts, 1, "a default options struct should have been created")
		assert.NotNil(t, newOpts[0].Monitor, "monitor should be injected")
	})

	t.Run("injects monitor into all provided options", func(t *testing.T) {
		t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB")

		optA := options.Client()
		optB := options.Client()
		mockCtx := hooktest.NewMockHookContext(t.Context())

		BeforeConnect(mockCtx, t.Context(), optA, optB)

		newOpts, ok := mockCtx.GetParam(1).([]*options.ClientOptions)
		require.True(t, ok, "param 1 should be updated with a []*options.ClientOptions")
		require.Len(t, newOpts, 2)
		assert.NotNil(t, newOpts[0].Monitor, "monitor should be injected into first option")
		assert.NotNil(t, newOpts[1].Monitor, "monitor should be injected into second option")
	})

	t.Run("does not overwrite an existing monitor", func(t *testing.T) {
		t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB")

		existing := &event.CommandMonitor{}
		opt := options.Client().SetMonitor(existing)
		mockCtx := hooktest.NewMockHookContext(t.Context())

		BeforeConnect(mockCtx, t.Context(), opt)

		newOpts, ok := mockCtx.GetParam(1).([]*options.ClientOptions)
		require.True(t, ok)
		require.Len(t, newOpts, 1)
		assert.Same(t, existing, newOpts[0].Monitor, "existing monitor should be left untouched")
	})

	t.Run("does nothing when instrumentation is disabled", func(t *testing.T) {
		t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "MONGODB")

		mockCtx := hooktest.NewMockHookContext(t.Context())

		BeforeConnect(mockCtx, t.Context())

		assert.Nil(t, mockCtx.GetParam(1), "param 1 (opts) should be left untouched when instrumentation is disabled")
	})
}

func TestBeforeNewClient(t *testing.T) {
	t.Run("injects monitor when opts is empty", func(t *testing.T) {
		t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB")

		mockCtx := hooktest.NewMockHookContext()

		BeforeNewClient(mockCtx)

		newOpts, ok := mockCtx.GetParam(0).([]*options.ClientOptions)
		require.True(t, ok, "param 0 should be updated with a []*options.ClientOptions")
		require.Len(t, newOpts, 1, "a default options struct should have been created")
		assert.NotNil(t, newOpts[0].Monitor, "monitor should be injected")
	})

	t.Run("injects monitor into all provided options", func(t *testing.T) {
		t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB")

		optA := options.Client()
		optB := options.Client()
		mockCtx := hooktest.NewMockHookContext()

		BeforeNewClient(mockCtx, optA, optB)

		newOpts, ok := mockCtx.GetParam(0).([]*options.ClientOptions)
		require.True(t, ok, "param 0 should be updated with a []*options.ClientOptions")
		require.Len(t, newOpts, 2)
		assert.NotNil(t, newOpts[0].Monitor, "monitor should be injected into first option")
		assert.NotNil(t, newOpts[1].Monitor, "monitor should be injected into second option")
	})

	t.Run("does not overwrite an existing monitor", func(t *testing.T) {
		t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB")

		existing := &event.CommandMonitor{}
		opt := options.Client().SetMonitor(existing)
		mockCtx := hooktest.NewMockHookContext()

		BeforeNewClient(mockCtx, opt)

		newOpts, ok := mockCtx.GetParam(0).([]*options.ClientOptions)
		require.True(t, ok)
		require.Len(t, newOpts, 1)
		assert.Same(t, existing, newOpts[0].Monitor, "existing monitor should be left untouched")
	})

	t.Run("does nothing when instrumentation is disabled", func(t *testing.T) {
		t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "MONGODB")

		mockCtx := hooktest.NewMockHookContext()

		BeforeNewClient(mockCtx)

		assert.Equal(t, 0, mockCtx.GetParamCount(), "no param should be set when instrumentation is disabled")
	})
}
