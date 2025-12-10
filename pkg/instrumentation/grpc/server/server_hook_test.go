// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
)

// mockHookContext implements inst.HookContext for testing
type mockHookContext struct {
	params     []interface{}
	data       map[string]interface{}
	returnVals []interface{}
	skipCall   bool
}

func newMockHookContext(params ...interface{}) *mockHookContext {
	return &mockHookContext{
		params: params,
		data:   make(map[string]interface{}),
	}
}

func (m *mockHookContext) SetSkipCall(skip bool)                  { m.skipCall = skip }
func (m *mockHookContext) IsSkipCall() bool                       { return m.skipCall }
func (m *mockHookContext) SetData(data interface{})               { m.data["_default"] = data }
func (m *mockHookContext) GetData() interface{}                   { return m.data["_default"] }
func (m *mockHookContext) GetKeyData(key string) interface{}      { return m.data[key] }
func (m *mockHookContext) SetKeyData(key string, val interface{}) { m.data[key] = val }
func (m *mockHookContext) HasKeyData(key string) bool             { _, ok := m.data[key]; return ok }
func (m *mockHookContext) GetParamCount() int                     { return len(m.params) }
func (m *mockHookContext) GetParam(idx int) interface{}           { return m.params[idx] }
func (m *mockHookContext) SetParam(idx int, val interface{})      { m.params[idx] = val }
func (m *mockHookContext) GetReturnValCount() int                 { return len(m.returnVals) }
func (m *mockHookContext) GetReturnVal(idx int) interface{}       { return m.returnVals[idx] }
func (m *mockHookContext) SetReturnVal(idx int, val interface{})  { m.returnVals[idx] = val }
func (m *mockHookContext) GetFuncName() string                    { return "TestFunc" }
func (m *mockHookContext) GetPackageName() string                 { return "test.package" }

func TestBeforeNewServer(t *testing.T) {
	// Setup trace exporter
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()

	tests := []struct {
		name          string
		opts          []grpc.ServerOption
		enabledEnv    bool
		expectHandler bool
	}{
		{
			name:          "no options",
			opts:          []grpc.ServerOption{},
			enabledEnv:    true,
			expectHandler: true,
		},
		{
			name: "with existing options",
			opts: []grpc.ServerOption{
				grpc.MaxRecvMsgSize(1024),
			},
			enabledEnv:    true,
			expectHandler: true,
		},
		{
			name:          "instrumentation disabled",
			opts:          []grpc.ServerOption{},
			enabledEnv:    false,
			expectHandler: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enabledEnv {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")
			} else {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "grpc")
			}

			ictx := newMockHookContext(tt.opts)
			BeforeNewServer(ictx, tt.opts)

			newOpts, ok := ictx.GetParam(0).([]grpc.ServerOption)
			require.True(t, ok)

			if tt.expectHandler {
				// Should have added stats handler
				assert.Greater(t, len(newOpts), len(tt.opts), "Expected stats handler to be added")
			} else {
				// Should not modify options when disabled
				assert.Equal(t, len(tt.opts), len(newOpts))
			}
		})
	}
}

// TestAfterNewServer verifies the AfterNewServer hook handles server creation
// without panicking. This hook is primarily for debug logging and doesn't modify state,
// so we verify it gracefully handles various server states.
func TestAfterNewServer(t *testing.T) {
	tests := []struct {
		name       string
		enabledEnv bool
		server     *grpc.Server
	}{
		{
			name:       "valid server with instrumentation enabled",
			enabledEnv: true,
			server:     grpc.NewServer(),
		},
		{
			name:       "nil server with instrumentation enabled",
			enabledEnv: true,
			server:     nil,
		},
		{
			name:       "valid server with instrumentation disabled",
			enabledEnv: false,
			server:     grpc.NewServer(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enabledEnv {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")
			} else {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "grpc")
			}

			// Cleanup server if created
			if tt.server != nil {
				defer tt.server.Stop()
			}

			ictx := newMockHookContext()

			// Verify the hook doesn't panic and handles gracefully
			assert.NotPanics(t, func() {
				AfterNewServer(ictx, tt.server)
			}, "AfterNewServer should not panic")
		})
	}
}

func TestServerStatsHandler_TagRPC(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")

	// Initialize instrumentation first
	initInstrumentation()

	// Setup trace exporter AFTER initialization
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	oldTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(oldTP)
	}()

	// Re-initialize to use new tracer provider
	tracer = tp.Tracer(instrumentationName, trace.WithInstrumentationVersion(instrumentationVersion))

	handler := newServerStatsHandler()

	tests := []struct {
		name           string
		fullMethodName string
		incomingMD     metadata.MD
	}{
		{
			name:           "valid method",
			fullMethodName: "/grpc.health.v1.Health/Check",
		},
		{
			name:           "with trace context",
			fullMethodName: "/grpc.testing.TestService/UnaryCall",
			incomingMD: metadata.MD{
				"traceparent": []string{"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0aa902b7-01"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.incomingMD != nil {
				ctx = metadata.NewIncomingContext(ctx, tt.incomingMD)
			}

			info := &stats.RPCTagInfo{
				FullMethodName: tt.fullMethodName,
			}

			// TagRPC creates the span
			newCtx := handler.TagRPC(ctx, info)
			assert.NotNil(t, newCtx)

			// Verify gRPC context was set
			gctx := newCtx.Value(gRPCContextKey{})
			assert.NotNil(t, gctx, "Expected gRPC context to be set")

			// End the RPC to export the span
			handler.HandleRPC(newCtx, &stats.End{
				BeginTime: time.Now().Add(-100 * time.Millisecond),
				EndTime:   time.Now(),
			})

			// Now verify span was exported
			spans := exporter.GetSpans()
			assert.NotEmpty(t, spans, "Expected span to be created and exported")
			if len(spans) > 0 {
				assert.Equal(t, tt.fullMethodName[1:], spans[0].Name) // Remove leading /
			}

			exporter.Reset()
		})
	}
}

func TestServerStatsHandler_Integration(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")

	// Initialize instrumentation
	initInstrumentation()

	// Create instrumented server
	opts := []grpc.ServerOption{}
	ictx := newMockHookContext(opts)
	BeforeNewServer(ictx, opts)

	newOpts := ictx.GetParam(0).([]grpc.ServerOption)
	assert.Greater(t, len(newOpts), 0, "Expected stats handler to be added")

	// Verify hook behavior
	server := grpc.NewServer(newOpts...)
	defer server.Stop()
	assert.NotNil(t, server)
}

func TestServerStatsHandler_TagConn(t *testing.T) {
	handler := newServerStatsHandler()

	ctx := context.Background()
	info := &stats.ConnTagInfo{
		LocalAddr: &net.TCPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: 50051,
		},
	}

	newCtx := handler.TagConn(ctx, info)
	assert.NotNil(t, newCtx)
}

func TestServerStatsHandler_HandleConn(t *testing.T) {
	handler := newServerStatsHandler()

	ctx := context.Background()

	// Should not panic
	handler.HandleConn(ctx, &stats.ConnBegin{})
}

func TestServerStatsHandler_OTELExporterFiltering(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")

	// Initialize instrumentation
	initInstrumentation()

	// Setup trace exporter
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	oldTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(oldTP)
	}()

	// Re-initialize to use new tracer provider
	tracer = tp.Tracer(instrumentationName, trace.WithInstrumentationVersion(instrumentationVersion))

	handler := newServerStatsHandler()

	tests := []struct {
		name             string
		fullMethodName   string
		shouldInstrument bool
	}{
		{
			name:             "OTLP trace exporter - should skip",
			fullMethodName:   "/opentelemetry.proto.collector.trace.v1.TraceService/Export",
			shouldInstrument: false,
		},
		{
			name:             "OTLP metric exporter - should skip",
			fullMethodName:   "/opentelemetry.proto.collector.metrics.v1.MetricsService/Export",
			shouldInstrument: false,
		},
		{
			name:             "OTLP log exporter - should skip",
			fullMethodName:   "/opentelemetry.proto.collector.logs.v1.LogsService/Export",
			shouldInstrument: false,
		},
		{
			name:             "regular gRPC call - should instrument",
			fullMethodName:   "/grpc.testing.TestService/UnaryCall",
			shouldInstrument: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			info := &stats.RPCTagInfo{
				FullMethodName: tt.fullMethodName,
			}

			// TagRPC creates the span (or skips for OTLP)
			newCtx := handler.TagRPC(ctx, info)
			assert.NotNil(t, newCtx)

			if tt.shouldInstrument {
				// Verify gRPC context was set
				gctx := newCtx.Value(gRPCContextKey{})
				assert.NotNil(t, gctx, "Expected gRPC context to be set for regular calls")

				// End the RPC to export the span
				handler.HandleRPC(newCtx, &stats.End{
					BeginTime: time.Now().Add(-100 * time.Millisecond),
					EndTime:   time.Now(),
				})

				// Verify span was created
				spans := exporter.GetSpans()
				assert.NotEmpty(t, spans, "Expected span for regular call")
			} else {
				// Verify gRPC context was NOT set (instrumentation skipped)
				gctx := newCtx.Value(gRPCContextKey{})
				assert.Nil(t, gctx, "Expected no gRPC context for OTLP exporter calls")

				// Verify no span was created
				spans := exporter.GetSpans()
				assert.Empty(t, spans, "Expected no span for OTLP exporter calls")
			}

			exporter.Reset()
		})
	}
}
