// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package mongodb

import (
	"context"
	"net/http"
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

func TestChainMonitors(t *testing.T) {
	t.Run("invokes both monitors' callbacks", func(t *testing.T) {
		var userStarted, userSucceeded, userFailed bool
		var otelStarted, otelSucceeded, otelFailed bool

		user := &event.CommandMonitor{
			Started:   func(context.Context, *event.CommandStartedEvent) { userStarted = true },
			Succeeded: func(context.Context, *event.CommandSucceededEvent) { userSucceeded = true },
			Failed:    func(context.Context, *event.CommandFailedEvent) { userFailed = true },
		}
		otel := &event.CommandMonitor{
			Started:   func(context.Context, *event.CommandStartedEvent) { otelStarted = true },
			Succeeded: func(context.Context, *event.CommandSucceededEvent) { otelSucceeded = true },
			Failed:    func(context.Context, *event.CommandFailedEvent) { otelFailed = true },
		}

		chained := chainMonitors(user, otel)
		chained.Started(context.Background(), &event.CommandStartedEvent{})
		chained.Succeeded(context.Background(), &event.CommandSucceededEvent{})
		chained.Failed(context.Background(), &event.CommandFailedEvent{})

		assert.True(t, userStarted)
		assert.True(t, userSucceeded)
		assert.True(t, userFailed)
		assert.True(t, otelStarted)
		assert.True(t, otelSucceeded)
		assert.True(t, otelFailed)
	})

	t.Run("does not panic when either monitor has nil callback fields", func(t *testing.T) {
		user := &event.CommandMonitor{}
		otel := &event.CommandMonitor{
			Started: func(context.Context, *event.CommandStartedEvent) {},
		}

		chained := chainMonitors(user, otel)
		assert.NotPanics(t, func() {
			chained.Started(context.Background(), &event.CommandStartedEvent{})
			chained.Succeeded(context.Background(), &event.CommandSucceededEvent{})
			chained.Failed(context.Background(), &event.CommandFailedEvent{})
		})
	})
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

	t.Run("injects a single trailing monitor when none of the provided options has one", func(t *testing.T) {
		t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB_V2")

		optA := options.Client()
		optB := options.Client()
		mockCtx := hooktest.NewMockHookContext()

		BeforeConnect(mockCtx, optA, optB)

		newOpts, ok := mockCtx.GetParam(0).([]*options.ClientOptions)
		require.True(t, ok, "param 0 should be updated with a []*options.ClientOptions")
		require.Len(t, newOpts, 3, "a trailing options struct carrying the monitor should have been appended")
		assert.Nil(t, newOpts[0].Monitor, "original first option should be left untouched")
		assert.Nil(t, newOpts[1].Monitor, "original second option should be left untouched")
		assert.NotNil(t, newOpts[2].Monitor, "monitor should be injected into the appended trailing option")
	})

	t.Run("chains an existing monitor with the otel monitor instead of replacing it", func(t *testing.T) {
		t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB_V2")

		var userStarted, userSucceeded, userFailed bool
		existing := &event.CommandMonitor{
			Started:   func(context.Context, *event.CommandStartedEvent) { userStarted = true },
			Succeeded: func(context.Context, *event.CommandSucceededEvent) { userSucceeded = true },
			Failed:    func(context.Context, *event.CommandFailedEvent) { userFailed = true },
		}
		opt := options.Client().SetMonitor(existing)
		mockCtx := hooktest.NewMockHookContext()

		BeforeConnect(mockCtx, opt)

		newOpts, ok := mockCtx.GetParam(0).([]*options.ClientOptions)
		require.True(t, ok)
		merged := options.MergeClientOptions(newOpts...)
		require.NotNil(t, merged.Monitor)
		assert.NotSame(
			t,
			existing,
			merged.Monitor,
			"the effective monitor should be a chained wrapper, not the user's original",
		)

		merged.Monitor.Started(context.Background(), &event.CommandStartedEvent{})
		merged.Monitor.Succeeded(context.Background(), &event.CommandSucceededEvent{})
		merged.Monitor.Failed(context.Background(), &event.CommandFailedEvent{})
		assert.True(t, userStarted, "user's Started callback should still fire")
		assert.True(t, userSucceeded, "user's Succeeded callback should still fire")
		assert.True(t, userFailed, "user's Failed callback should still fire")
	})

	t.Run(
		"chains a user monitor set on an earlier option instead of letting the injected monitor override it",
		func(t *testing.T) {
			// Regression test for https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/1148:
			// mongo.NewClient resolves opts via options.MergeClientOptions, which keeps
			// the last non-nil value of each field across the slice. Injecting into a
			// later, still-nil struct used to let the OTel monitor win the merge over a
			// user's own CommandMonitor set on an earlier struct, dropping it entirely.
			t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB_V2")

			var userStarted bool
			existing := &event.CommandMonitor{
				Started: func(context.Context, *event.CommandStartedEvent) { userStarted = true },
			}
			base := options.Client().SetMonitor(existing)
			uriOpts := options.Client().ApplyURI("mongodb://localhost:27017")
			mockCtx := hooktest.NewMockHookContext()

			BeforeConnect(mockCtx, base, uriOpts)

			newOpts, ok := mockCtx.GetParam(0).([]*options.ClientOptions)
			require.True(t, ok)
			merged := options.MergeClientOptions(newOpts...)
			require.NotNil(t, merged.Monitor)

			merged.Monitor.Started(context.Background(), &event.CommandStartedEvent{})
			assert.True(t, userStarted, "user's monitor set on an earlier option must still fire")
		},
	)

	t.Run("carries a caller's custom HTTPClient through injection instead of losing it to options.Client()'s default", func(t *testing.T) {
		t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "MONGODB_V2")

		customClient := &http.Client{}
		base := options.Client().SetHTTPClient(customClient)
		mockCtx := hooktest.NewMockHookContext()

		BeforeConnect(mockCtx, base)

		newOpts, ok := mockCtx.GetParam(0).([]*options.ClientOptions)
		require.True(t, ok)
		merged := options.MergeClientOptions(newOpts...)
		assert.Same(t, customClient, merged.HTTPClient,
			"caller's HTTPClient must survive injection, not be replaced by the appended element's default")
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
		require.Len(t, newOpts, 2, "a trailing options struct carrying the monitor should have been appended")
		assert.Nil(t, newOpts[0], "first option should still be nil")
		assert.NotNil(t, newOpts[1].Monitor, "monitor should be injected into the appended trailing option")
	})
}
