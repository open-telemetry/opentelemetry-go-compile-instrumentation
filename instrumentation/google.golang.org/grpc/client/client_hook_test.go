// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

func TestBeforeNewClient(t *testing.T) {
	// Setup trace exporter
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tests := []struct {
		name          string
		target        string
		opts          []grpc.DialOption
		enabledEnv    bool
		expectHandler bool
	}{
		{
			name:          "no options",
			target:        "localhost:50051",
			opts:          []grpc.DialOption{},
			enabledEnv:    true,
			expectHandler: true,
		},
		{
			name:   "with existing options",
			target: "localhost:50051",
			opts: []grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			},
			enabledEnv:    true,
			expectHandler: true,
		},
		{
			name:          "instrumentation disabled",
			target:        "localhost:50051",
			opts:          []grpc.DialOption{},
			enabledEnv:    false,
			expectHandler: false,
		},
		{
			name:          "nil options slice",
			target:        "localhost:50051",
			opts:          nil,
			enabledEnv:    true,
			expectHandler: true,
		},
		{
			name:          "empty target with options",
			target:        "",
			opts:          []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
			enabledEnv:    true,
			expectHandler: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enabledEnv {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")
			} else {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "grpc")
			}

			ictx := hooktest.NewMockHookContext(tt.target, tt.opts)

			// Verify no panic even with edge cases
			assert.NotPanics(t, func() {
				BeforeNewClient(ictx, tt.target, tt.opts...)
			}, "BeforeNewClient should not panic")

			newOpts, ok := ictx.GetParam(newClientOptionsParamIndex).([]grpc.DialOption)
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

// TestAfterNewClient verifies the AfterNewClient hook handles various connection outcomes
// without panicking. This hook is primarily for debug logging and doesn't modify state,
// so we verify it gracefully handles both success and error cases.
func TestAfterNewClient(t *testing.T) {
	tests := []struct {
		name       string
		enabledEnv bool
		conn       *grpc.ClientConn
		err        error
	}{
		{
			name:       "successful connection with instrumentation enabled",
			enabledEnv: true,
			conn:       &grpc.ClientConn{},
			err:        nil,
		},
		{
			name:       "connection error with instrumentation enabled",
			enabledEnv: true,
			conn:       nil,
			err:        assert.AnError,
		},
		{
			name:       "successful connection with instrumentation disabled",
			enabledEnv: false,
			conn:       &grpc.ClientConn{},
			err:        nil,
		},
		{
			name:       "connection error with instrumentation disabled",
			enabledEnv: false,
			conn:       nil,
			err:        assert.AnError,
		},
		{
			name:       "both nil conn and nil error",
			enabledEnv: true,
			conn:       nil,
			err:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enabledEnv {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")
			} else {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "grpc")
			}

			ictx := hooktest.NewMockHookContext()

			// Verify the hook doesn't panic and handles gracefully
			assert.NotPanics(t, func() {
				AfterNewClient(ictx, tt.conn, tt.err)
			}, "AfterNewClient should not panic")
		})
	}
}

func TestBeforeDialContext(t *testing.T) {
	// Setup trace exporter
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tests := []struct {
		name          string
		target        string
		opts          []grpc.DialOption
		enabledEnv    bool
		expectHandler bool
	}{
		{
			name:          "no options",
			target:        "localhost:50051",
			opts:          []grpc.DialOption{},
			enabledEnv:    true,
			expectHandler: true,
		},
		{
			name:   "with existing options",
			target: "localhost:50051",
			opts: []grpc.DialOption{
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			},
			enabledEnv:    true,
			expectHandler: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enabledEnv {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")
			} else {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "grpc")
			}

			ctx := t.Context()
			ictx := hooktest.NewMockHookContext(ctx, tt.target, tt.opts)
			BeforeDialContext(ictx, ctx, tt.target, tt.opts...)

			newOpts, ok := ictx.GetParam(dialOptionsParamIndex).([]grpc.DialOption)
			require.True(t, ok)

			if tt.expectHandler {
				// Should have added stats handler
				assert.Greater(t, len(newOpts), len(tt.opts), "Expected stats handler to be added")
			}
		})
	}
}

// TestAfterDialContext verifies the AfterDialContext hook handles various connection outcomes
// without panicking. This hook is primarily for debug logging and doesn't modify state,
// so we verify it gracefully handles both success and error cases.
func TestAfterDialContext(t *testing.T) {
	tests := []struct {
		name       string
		enabledEnv bool
		conn       *grpc.ClientConn
		err        error
	}{
		{
			name:       "successful connection with instrumentation enabled",
			enabledEnv: true,
			conn:       &grpc.ClientConn{},
			err:        nil,
		},
		{
			name:       "connection error with instrumentation enabled",
			enabledEnv: true,
			conn:       nil,
			err:        assert.AnError,
		},
		{
			name:       "successful connection with instrumentation disabled",
			enabledEnv: false,
			conn:       &grpc.ClientConn{},
			err:        nil,
		},
		{
			name:       "connection error with instrumentation disabled",
			enabledEnv: false,
			conn:       nil,
			err:        assert.AnError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enabledEnv {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")
			} else {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "grpc")
			}

			ictx := hooktest.NewMockHookContext()

			// Verify the hook doesn't panic and handles gracefully
			assert.NotPanics(t, func() {
				AfterDialContext(ictx, tt.conn, tt.err)
			}, "AfterDialContext should not panic")
		})
	}
}

func TestClientStatsHandler_TagRPC(t *testing.T) {
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
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(oldTP)
	})

	// Re-initialize to use new tracer provider
	tracer = tp.Tracer(instrumentationName, trace.WithInstrumentationVersion(runtime.ModuleVersion()))

	handler := newClientStatsHandler()

	tests := []struct {
		name           string
		fullMethodName string
	}{
		{
			name:           "valid method",
			fullMethodName: "/grpc.health.v1.Health/Check",
		},
		{
			name:           "test service method",
			fullMethodName: "/grpc.testing.TestService/UnaryCall",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
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

func TestClientStatsHandler_Integration(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")

	// Initialize instrumentation
	initInstrumentation()

	// Create instrumented client using NewClient
	target := "localhost:50051"
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	ictx := hooktest.NewMockHookContext(target, opts)
	BeforeNewClient(ictx, target, opts...)

	newOpts := ictx.GetParam(newClientOptionsParamIndex).([]grpc.DialOption)
	assert.Greater(t, len(newOpts), len(opts), "Expected stats handler to be added")

	// Test DialContext as well
	ctx := t.Context()
	ictx2 := hooktest.NewMockHookContext(ctx, target, opts)
	BeforeDialContext(ictx2, ctx, target, opts...)

	newOpts2 := ictx2.GetParam(dialOptionsParamIndex).([]grpc.DialOption)
	assert.Greater(t, len(newOpts2), len(opts), "Expected stats handler to be added for DialContext")
}

func TestClientStatsHandler_HandleRPC_PayloadEvents(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")

	initInstrumentation()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	oldTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(oldTP)
	})
	tracer = tp.Tracer(instrumentationName, trace.WithInstrumentationVersion(runtime.ModuleVersion()))

	handler := newClientStatsHandler()

	ctx := handler.TagRPC(t.Context(), &stats.RPCTagInfo{
		FullMethodName: "/grpc.testing.TestService/UnaryCall",
	})
	require.NotNil(t, ctx)

	// Drive the message-lifecycle events HandleRPC handles before End.
	// On the client, OutPayload counts outgoing requests and InPayload
	// counts incoming responses. These branches were previously untested.
	handler.HandleRPC(ctx, &stats.Begin{BeginTime: time.Now()})
	handler.HandleRPC(ctx, &stats.OutPayload{Length: 256})
	handler.HandleRPC(ctx, &stats.InPayload{Length: 128})
	handler.HandleRPC(ctx, &stats.InPayload{Length: 64})
	handler.HandleRPC(ctx, &stats.OutHeader{})

	gctx, ok := ctx.Value(gRPCContextKey{}).(*gRPCContext)
	require.True(t, ok, "expected gRPC context to be set by TagRPC")
	assert.Equal(t, int64(1), gctx.outMessages, "one OutPayload event should be counted")
	assert.Equal(t, int64(2), gctx.inMessages, "two InPayload events should be counted")

	handler.HandleRPC(ctx, &stats.End{
		BeginTime: time.Now().Add(-50 * time.Millisecond),
		EndTime:   time.Now(),
	})

	spans := exporter.GetSpans()
	assert.NotEmpty(t, spans, "expected the RPC span to be exported after End")
}

func TestClientStatsHandler_HandleRPC_WithError(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")

	initInstrumentation()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	oldTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(oldTP)
	})
	tracer = tp.Tracer(instrumentationName, trace.WithInstrumentationVersion(runtime.ModuleVersion()))

	handler := newClientStatsHandler()

	ctx := handler.TagRPC(t.Context(), &stats.RPCTagInfo{
		FullMethodName: "/grpc.testing.TestService/UnaryCall",
	})
	require.NotNil(t, ctx)

	handler.HandleRPC(ctx, &stats.End{
		BeginTime: time.Now().Add(-50 * time.Millisecond),
		EndTime:   time.Now(),
		Error:     status.Error(codes.Unavailable, "connection refused"),
	})

	spans := exporter.GetSpans()
	require.NotEmpty(t, spans, "expected span to be exported")
	span := spans[0]
	assert.Equal(t, otelcodes.Error, span.Status.Code, "errored RPC should set span status to Error")

	events := span.Events
	require.Len(t, events, 1, "expected one exception event for the recorded error")
	assert.Equal(t, "exception", events[0].Name)
}

func TestClientStatsHandler_HandleRPC_NilContextIsNoop(t *testing.T) {
	handler := newClientStatsHandler()

	assert.NotPanics(t, func() {
		handler.HandleRPC(t.Context(), &stats.OutPayload{Length: 10})
		handler.HandleRPC(t.Context(), &stats.InPayload{Length: 10})
		handler.HandleRPC(t.Context(), &stats.OutHeader{})
	})
}

func TestClientStatsHandler_TagConn(t *testing.T) {
	handler := newClientStatsHandler()

	ctx := t.Context()
	info := &stats.ConnTagInfo{
		RemoteAddr: &net.TCPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: 50051,
		},
	}

	newCtx := handler.TagConn(ctx, info)
	assert.NotNil(t, newCtx)
}

func TestClientStatsHandler_HandleConn(t *testing.T) {
	handler := newClientStatsHandler()

	ctx := t.Context()

	// Should not panic
	handler.HandleConn(ctx, &stats.ConnBegin{})
}

func TestClientStatsHandler_OTELExporterFiltering(t *testing.T) {
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
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(oldTP)
	})

	// Re-initialize to use new tracer provider
	tracer = tp.Tracer(instrumentationName, trace.WithInstrumentationVersion(runtime.ModuleVersion()))

	handler := newClientStatsHandler()

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
			ctx := t.Context()
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

// TestClientStatsHandler_SkippedRPC_DoesNotTouchCallerSpan is the regression test for the
// caller-span corruption bug. When TagRPC opts out of an OTLP export path, HandleRPC must
// be a complete no-op: it must not stamp gRPC attributes onto, set the status of, or end
// any span that happens to be active on the caller's context.
func TestClientStatsHandler_SkippedRPC_DoesNotTouchCallerSpan(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")

	initInstrumentation()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	oldTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(oldTP)
	})
	tracer = tp.Tracer(instrumentationName, trace.WithInstrumentationVersion(runtime.ModuleVersion()))

	// Simulate the caller starting its own span — e.g. "graceful-shutdown".
	baseCtx, callerSpan := tp.Tracer("test").Start(t.Context(), "graceful-shutdown")

	// TagRPC skips instrumentation for the OTLP export path and returns ctx unchanged.
	handler := newClientStatsHandler()
	skippedCtx := handler.TagRPC(baseCtx, &stats.RPCTagInfo{
		FullMethodName: "/opentelemetry.proto.collector.trace.v1.TraceService/Export",
	})

	// The returned context must carry no gRPCContext — the skip sentinel.
	require.Nil(t, skippedCtx.Value(gRPCContextKey{}), "TagRPC must not attach gRPCContext for OTLP paths")

	// Drive every HandleRPC branch that previously touched the span directly.
	handler.HandleRPC(skippedCtx, &stats.Begin{BeginTime: time.Now()})
	handler.HandleRPC(skippedCtx, &stats.OutHeader{})
	handler.HandleRPC(skippedCtx, &stats.OutPayload{Length: 64})
	handler.HandleRPC(skippedCtx, &stats.InPayload{Length: 128})
	handler.HandleRPC(skippedCtx, &stats.End{
		BeginTime: time.Now().Add(-10 * time.Millisecond),
		EndTime:   time.Now(),
	})

	// The caller span must still be recording — HandleRPC must not have ended it.
	require.True(t, callerSpan.IsRecording(), "HandleRPC must not end the caller's span on a skipped RPC")

	// End it ourselves and check the export is clean.
	callerSpan.End()
	spans := exporter.GetSpans()
	require.Len(t, spans, 1, "only the caller span should be exported")
	assert.Equal(t, "graceful-shutdown", spans[0].Name, "exported span must be the caller's span")

	// Verify no gRPC status attribute was written onto the caller span.
	for _, attr := range spans[0].Attributes {
		assert.NotEqual(t, "rpc.grpc.status_code", string(attr.Key),
			"HandleRPC must not stamp rpc.grpc.status_code onto the caller's span")
	}
}

// TestClientStatsHandler_NilGRPCContext_IsSafeNoOp pins the invariant that the early
// return in HandleRPC is load-bearing for memory safety, not just correctness. Every
// branch below that guard dereferences gctx without a nil check (gctx.outMessages,
// gctx.metricAttrs), so moving or dropping the guard turns a skipped RPC into a nil
// pointer panic inside the instrumented application rather than a silent no-op.
func TestClientStatsHandler_NilGRPCContext_IsSafeNoOp(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")

	initInstrumentation()

	handler := newClientStatsHandler()
	ctx := t.Context() // no gRPCContext attached, and no span either

	require.Nil(t, ctx.Value(gRPCContextKey{}), "precondition: context carries no gRPCContext")

	require.NotPanics(t, func() {
		handler.HandleRPC(ctx, &stats.Begin{BeginTime: time.Now()})
		handler.HandleRPC(ctx, &stats.OutHeader{})
		handler.HandleRPC(ctx, &stats.OutPayload{Length: 64})
		handler.HandleRPC(ctx, &stats.InPayload{Length: 128})
		handler.HandleRPC(ctx, &stats.End{
			BeginTime: time.Now().Add(-10 * time.Millisecond),
			EndTime:   time.Now(),
		})
	}, "HandleRPC must return before dereferencing a nil gRPCContext")
}
