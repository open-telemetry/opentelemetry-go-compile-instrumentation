// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package k8s_client_go

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst/insttest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

func setupTestTracer(t *testing.T) (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return sr, tp
}

func TestK8SClientGoEnabler(t *testing.T) {
	tests := []struct {
		name     string
		setupEnv func(t *testing.T)
		expected bool
	}{
		{
			name: "enabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "k8s_client_go")
			},
			expected: true,
		},
		{
			name: "disabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "k8s_client_go")
			},
			expected: false,
		},
		{
			name: "not in enabled list",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
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
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp,k8s_client_go,grpc")
			},
			expected: true,
		},
		{
			name: "disabled with multiple instrumentations",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "k8s_client_go,grpc")
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupEnv(t)

			enabler := k8SClientGoEnabler{}
			result := enabler.Enable()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInstrumentationConstants(t *testing.T) {
	assert.Equal(t,
		"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/k8s-client-go",
		instrumentationName,
	)
	assert.Equal(t, "K8S_CLIENT_GO", instrumentationKey)
}

func TestModuleVersion(t *testing.T) {
	version := moduleVersion()
	// In test mode, version should be "dev" since there's no proper build info
	assert.NotEmpty(t, version)
}

func TestBeforeProcessDeltas(t *testing.T) {
	for _, tt := range []struct {
		name        string
		setupEnv    func(t *testing.T)
		expectSpans bool
	}{
		{
			name: "instrumentation enabled",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "k8s_client_go")
			},
			expectSpans: true,
		},
		{
			name: "instrumentation disabled",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "k8s_client_go")
			},
			expectSpans: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			initOnce = *new(sync.Once)

			tt.setupEnv(t)
			sr, _ := setupTestTracer(t)

			handler := cache.ResourceEventHandlerFuncs{}

			mockCtx := insttest.NewMockHookContext(handler)
			beforeProcessDeltas(
				mockCtx,
				handler,
				cache.NewStore(cache.DeletionHandlingMetaNamespaceKeyFunc),
				[]cache.Delta{},
				false,
			)

			handlerUpdated := mockCtx.GetParam(0).(cache.ResourceEventHandler)
			handlerUpdated.OnAdd(&corev1.Pod{}, false)

			startedSpans := sr.Started()
			endedSpans := sr.Ended()
			if tt.expectSpans {
				require.Len(t, startedSpans, 2, "should have started two spans")
				require.Len(t, endedSpans, 1, "only the span from handler.OnAdd should have ended")
			} else {
				require.Len(t, endedSpans, 0, "no spans should be emitted")
			}
		})
	}
}

func TestAfterProcessDeltas(t *testing.T) {
	for _, tt := range []struct {
		name         string
		setupEnv     func(t *testing.T)
		setupContext func(*sdktrace.TracerProvider) inst.HookContext
		err          error
		validateSpan func(t *testing.T, spans []sdktrace.ReadOnlySpan)
	}{
		{
			name: "instrumentation enabled",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "k8s_client_go")
			},
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				_, span := testTracer.Start(context.Background(), "k8s.informer.objects.process", trace.WithSpanKind(trace.SpanKindInternal))

				mockCtx := insttest.NewMockHookContext()
				mockCtx.SetKeyData("span", span)
				return mockCtx
			},
			err: nil,
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				require.Len(t, spans, 1)
				span := spans[0]
				assert.Equal(t, codes.Unset, span.Status().Code)
			},
		},
		{
			name: "error callback",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "k8s_client_go")
			},
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				_, span := testTracer.Start(context.Background(), "k8s.informer.objects.process", trace.WithSpanKind(trace.SpanKindInternal))

				mockCtx := insttest.NewMockHookContext()
				mockCtx.SetKeyData("span", span)
				return mockCtx
			},
			err: errors.New("processing failed"),
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				require.Len(t, spans, 1)
				span := spans[0]
				assert.Equal(t, codes.Error, span.Status().Code)
				assert.Contains(t, span.Status().Description, "processing failed")

				// Check that error was recorded
				events := span.Events()
				require.Len(t, events, 1)
				assert.Equal(t, "exception", events[0].Name)
			},
		},
		{
			name: "no data in context",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "k8s_client_go")
			},
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				return insttest.NewMockHookContext()
			},
			err: nil,
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				// No span should be ended
				assert.Equal(t, 0, len(spans))
			},
		},
		{
			name: "instrumentation disabled",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "k8s_client_go")
			},
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				_, span := testTracer.Start(context.Background(), "k8s.informer.objects.process", trace.WithSpanKind(trace.SpanKindInternal))

				mockCtx := insttest.NewMockHookContext()
				mockCtx.SetKeyData("span", span)
				return mockCtx
			},
			err: nil,
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				// Span should not be ended because instrumentation is disabled
				assert.Equal(t, 0, len(spans))
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			initOnce = *new(sync.Once)

			tt.setupEnv(t)
			sr, tp := setupTestTracer(t)

			mockCtx := tt.setupContext(tp)
			afterProcessDeltas(mockCtx, tt.err)

			spans := sr.Ended()
			if tt.validateSpan != nil {
				tt.validateSpan(t, spans)
			}
		})
	}
}

func TestBeforeProcessDeltasInBatch(t *testing.T) {
	for _, tt := range []struct {
		name        string
		setupEnv    func(t *testing.T)
		expectSpans bool
	}{
		{
			name: "instrumentation enabled",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "k8s_client_go")
			},
			expectSpans: true,
		},
		{
			name: "instrumentation disabled",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "k8s_client_go")
			},
			expectSpans: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			initOnce = *new(sync.Once)

			tt.setupEnv(t)
			sr, _ := setupTestTracer(t)

			handler := cache.ResourceEventHandlerFuncs{}

			mockCtx := insttest.NewMockHookContext(handler)
			beforeProcessDeltasInBatch(
				mockCtx,
				handler,
				cache.NewStore(cache.DeletionHandlingMetaNamespaceKeyFunc),
				[]cache.Delta{},
				false,
			)

			handlerUpdated := mockCtx.GetParam(0).(cache.ResourceEventHandler)
			handlerUpdated.OnAdd(&corev1.Pod{}, false)

			startedSpans := sr.Started()
			endedSpans := sr.Ended()
			if tt.expectSpans {
				require.Len(t, startedSpans, 2, "should have started two spans")
				require.Len(t, endedSpans, 1, "only the span from handler.OnAdd should have ended")
			} else {
				require.Len(t, endedSpans, 0, "no spans should be emitted")
			}
		})
	}
}

func TestAfterProcessDeltasInBatch(t *testing.T) {
	for _, tt := range []struct {
		name         string
		setupEnv     func(t *testing.T)
		setupContext func(*sdktrace.TracerProvider) inst.HookContext
		err          error
		validateSpan func(t *testing.T, spans []sdktrace.ReadOnlySpan)
	}{
		{
			name: "instrumentation enabled",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "k8s_client_go")
			},
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				_, span := testTracer.Start(context.Background(), "k8s.informer.objects.process", trace.WithSpanKind(trace.SpanKindInternal))

				mockCtx := insttest.NewMockHookContext()
				mockCtx.SetKeyData("span", span)
				return mockCtx
			},
			err: nil,
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				require.Len(t, spans, 1)
				span := spans[0]
				assert.Equal(t, codes.Unset, span.Status().Code)
			},
		},
		{
			name: "error callback",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "k8s_client_go")
			},
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				_, span := testTracer.Start(context.Background(), "k8s.informer.objects.process", trace.WithSpanKind(trace.SpanKindInternal))

				mockCtx := insttest.NewMockHookContext()
				mockCtx.SetKeyData("span", span)
				return mockCtx
			},
			err: errors.New("processing failed"),
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				require.Len(t, spans, 1)
				span := spans[0]
				assert.Equal(t, codes.Error, span.Status().Code)
				assert.Contains(t, span.Status().Description, "processing failed")

				// Check that error was recorded
				events := span.Events()
				require.Len(t, events, 1)
				assert.Equal(t, "exception", events[0].Name)
			},
		},
		{
			name: "no data in context",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "k8s_client_go")
			},
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				return insttest.NewMockHookContext()
			},
			err: nil,
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				// No span should be ended
				assert.Equal(t, 0, len(spans))
			},
		},
		{
			name: "instrumentation disabled",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "k8s_client_go")
			},
			setupContext: func(tp *sdktrace.TracerProvider) inst.HookContext {
				testTracer := tp.Tracer(instrumentationName)
				_, span := testTracer.Start(context.Background(), "k8s.informer.objects.process", trace.WithSpanKind(trace.SpanKindInternal))

				mockCtx := insttest.NewMockHookContext()
				mockCtx.SetKeyData("span", span)
				return mockCtx
			},
			err: nil,
			validateSpan: func(t *testing.T, spans []sdktrace.ReadOnlySpan) {
				// Span should not be ended because instrumentation is disabled
				assert.Equal(t, 0, len(spans))
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			initOnce = *new(sync.Once)

			tt.setupEnv(t)
			sr, tp := setupTestTracer(t)

			mockCtx := tt.setupContext(tp)
			afterProcessDeltasInBatch(mockCtx, tt.err)

			spans := sr.Ended()
			if tt.validateSpan != nil {
				tt.validateSpan(t, spans)
			}
		})
	}
}
