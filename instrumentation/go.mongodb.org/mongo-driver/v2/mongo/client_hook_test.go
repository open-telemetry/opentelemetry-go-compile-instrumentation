// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package mongodb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
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
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB_V2")
			},
			expected: true,
		},
		{
			name: "disabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "MONGODB_V2")
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
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "REDIS,MONGODB_V2,GRPC")
			},
			expected: true,
		},
		{
			name: "disabled with multiple instrumentations",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "MONGODB_V2,GRPC")
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
		t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB_V2")

		mockCtx := hooktest.NewMockHookContext()

		BeforeConnect(mockCtx)

		newOpts, ok := mockCtx.GetParam(0).([]*options.ClientOptions)
		require.True(t, ok, "param 0 should be updated with a []*options.ClientOptions")
		require.Len(t, newOpts, 1, "a default options struct should have been created")
		assert.NotNil(t, newOpts[0].Monitor, "monitor should be injected")
	})

	t.Run("injects monitor into all provided options", func(t *testing.T) {
		t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB_V2")

		optA := options.Client()
		optB := options.Client()
		mockCtx := hooktest.NewMockHookContext()

		BeforeConnect(mockCtx, optA, optB)

		newOpts, ok := mockCtx.GetParam(0).([]*options.ClientOptions)
		require.True(t, ok, "param 0 should be updated with a []*options.ClientOptions")
		require.Len(t, newOpts, 2)
		assert.NotNil(t, newOpts[0].Monitor, "monitor should be injected into first option")
		assert.NotNil(t, newOpts[1].Monitor, "monitor should be injected into second option")
	})

	t.Run("does not overwrite an existing monitor", func(t *testing.T) {
		t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB_V2")

		existing := &event.CommandMonitor{}
		opt := options.Client().SetMonitor(existing)
		mockCtx := hooktest.NewMockHookContext()

		BeforeConnect(mockCtx, opt)

		newOpts, ok := mockCtx.GetParam(0).([]*options.ClientOptions)
		require.True(t, ok)
		require.Len(t, newOpts, 1)
		assert.Same(t, existing, newOpts[0].Monitor, "existing monitor should be left untouched")
	})

	t.Run("does nothing when instrumentation is disabled", func(t *testing.T) {
		t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "MONGODB_V2")

		mockCtx := hooktest.NewMockHookContext()

		BeforeConnect(mockCtx)

		assert.Nil(t, mockCtx.GetParam(0), "param 0 (opts) should be left untouched when instrumentation is disabled")
	})

	t.Run("does not panic on nil options", func(t *testing.T) {
		t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB_V2")

		mockCtx := hooktest.NewMockHookContext()

		BeforeConnect(mockCtx, nil)

		newOpts, ok := mockCtx.GetParam(0).([]*options.ClientOptions)
		require.True(t, ok, "param 0 should be updated with a []*options.ClientOptions")
		require.Len(t, newOpts, 1)
		assert.Nil(t, newOpts[0], "first option should still be nil")
	})
}
