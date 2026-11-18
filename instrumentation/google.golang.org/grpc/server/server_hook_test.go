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
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

func TestBeforeNewServer(t *testing.T) {
	// Setup trace exporter
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

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

			ictx := hooktest.NewMockHookContext(tt.opts)
			BeforeNewServer(ictx, tt.opts...)

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
				t.Cleanup(tt.server.Stop)
			}

			ictx := hooktest.NewMockHookContext()

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
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(oldTP)
	})

	// Re-initialize to use new tracer provider
	tracer = tp.Tracer(instrumentationName, trace.WithInstrumentationVersion(runtime.ModuleVersion()))

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
			ctx := t.Context()
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
	ictx := hooktest.NewMockHookContext(opts)
	BeforeNewServer(ictx, opts...)

	newOpts := ictx.GetParam(0).([]grpc.ServerOption)
	assert.Greater(t, len(newOpts), 0, "Expected stats handler to be added")

	// Verify hook behavior
	server := grpc.NewServer(newOpts...)
	t.Cleanup(server.Stop)
	assert.NotNil(t, server)
}

func TestServerStatsHandler_HandleRPC_PayloadEvents(t *testing.T) {
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

	handler := newServerStatsHandler()

	ctx := handler.TagRPC(t.Context(), &stats.RPCTagInfo{
		FullMethodName: "/grpc.testing.TestService/UnaryCall",
	})
	require.NotNil(t, ctx)

	// Drive the message-lifecycle events that HandleRPC handles before End.
	// These previously had no coverage; they must not panic and must count
	// messages on the gRPC context.
	handler.HandleRPC(ctx, &stats.Begin{BeginTime: time.Now()})
	handler.HandleRPC(ctx, &stats.InPayload{Length: 128})
	handler.HandleRPC(ctx, &stats.InPayload{Length: 64})
	handler.HandleRPC(ctx, &stats.OutPayload{Length: 256})
	handler.HandleRPC(ctx, &stats.OutHeader{})

	gctx, ok := ctx.Value(gRPCContextKey{}).(*gRPCContext)
	require.True(t, ok, "expected gRPC context to be set by TagRPC")
	assert.Equal(t, int64(2), gctx.inMessages, "two InPayload events should be counted")
	assert.Equal(t, int64(1), gctx.outMessages, "one OutPayload event should be counted")

	// End the RPC so the span is finished and exported.
	handler.HandleRPC(ctx, &stats.End{
		BeginTime: time.Now().Add(-50 * time.Millisecond),
		EndTime:   time.Now(),
	})

	spans := exporter.GetSpans()
	assert.NotEmpty(t, spans, "expected the RPC span to be exported after End")
}

func TestServerStatsHandler_HandleRPC_WithError(t *testing.T) {
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

	handler := newServerStatsHandler()

	ctx := handler.TagRPC(t.Context(), &stats.RPCTagInfo{
		FullMethodName: "/grpc.testing.TestService/UnaryCall",
	})
	require.NotNil(t, ctx)

	// End with an error exercises the error-status branch of HandleRPC.
	handler.HandleRPC(ctx, &stats.End{
		BeginTime: time.Now().Add(-50 * time.Millisecond),
		EndTime:   time.Now(),
		Error:     status.Error(codes.Internal, "boom"),
	})

	spans := exporter.GetSpans()
	require.NotEmpty(t, spans, "expected span to be exported")
	assert.Equal(t, otelcodes.Error, spans[0].Status.Code, "errored RPC should set span status to Error")

	events := spans[0].Events
	require.Len(t, events, 1, "expected one exception event for the recorded error")
	assert.Equal(t, "exception", events[0].Name)
}

func TestServerStatsHandler_HandleRPC_NilContextIsNoop(t *testing.T) {
	handler := newServerStatsHandler()

	// A context with no span / no gRPC context must not panic.
	assert.NotPanics(t, func() {
		handler.HandleRPC(t.Context(), &stats.InPayload{Length: 10})
		handler.HandleRPC(t.Context(), &stats.OutPayload{Length: 10})
		handler.HandleRPC(t.Context(), &stats.OutHeader{})
	})
}

// otherLayerStatsHandler mimics a stats.Handler from another tracing layer
// (for example otelgrpc) that the application registers in addition to otelc.
// grpc-go chains TagRPC in registration order, and otelc prepends its own
// handler, so this handler runs after otelc's and its span is the innermost
// span on the context that HandleRPC receives. Like otelgrpc, it ends its
// span when HandleRPC receives stats.End.
type otherLayerStatsHandler struct {
	tracer trace.Tracer
}

func (h *otherLayerStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	//nolint:spancheck // the span outlives TagRPC and ends in HandleRPC
	ctx, _ = h.tracer.Start(ctx, "other-layer/"+info.FullMethodName)
	return ctx
}

func (h *otherLayerStatsHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	if _, ok := rs.(*stats.End); ok {
		trace.SpanFromContext(ctx).End()
	}
}

func (h *otherLayerStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (h *otherLayerStatsHandler) HandleConn(context.Context, stats.ConnStats) {}

// TestServerStatsHandler_ChainedHandler_EndsOwnSpan is the regression test for the
// span leak with chained stats handlers. HandleRPC receives the final chained
// context, whose innermost span belongs to the handler that ran last in TagRPC.
// It must annotate and end the span TagRPC started, not the innermost span.
func TestServerStatsHandler_ChainedHandler_EndsOwnSpan(t *testing.T) {
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

	info := &stats.RPCTagInfo{
		FullMethodName: "/grpc.testing.TestService/UnaryCall",
	}

	// Chain TagRPC in the order grpc-go uses: otelc first, the other layer second.
	otelcHandler := newServerStatsHandler()
	otherHandler := &otherLayerStatsHandler{tracer: tp.Tracer("other-layer")}
	chainedCtx := otherHandler.TagRPC(otelcHandler.TagRPC(t.Context(), info), info)

	// OutHeader must write the peer address to otelc's span, not to the
	// innermost span. The peer context wraps the chained context, so it
	// still carries the other layer's span.
	peerCtx := peer.NewContext(chainedCtx, &peer.Peer{
		Addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 50051},
	})
	otelcHandler.HandleRPC(peerCtx, &stats.OutHeader{})
	otherHandler.HandleRPC(peerCtx, &stats.OutHeader{})

	end := &stats.End{
		BeginTime: time.Now().Add(-50 * time.Millisecond),
		EndTime:   time.Now(),
		Error:     status.Error(codes.Internal, "boom"),
	}

	// grpc-go calls HandleRPC on every handler with the final chained context.
	otelcHandler.HandleRPC(chainedCtx, end)

	// otelc must not end or annotate the other layer's span. The other layer
	// ends its own span after otelc, like the chained order does.
	assert.True(t, trace.SpanFromContext(chainedCtx).IsRecording(),
		"otelc must not end the other handler's span")

	// otelc's own span must be ended and exported with the gRPC status.
	spans := exporter.GetSpans()
	require.Len(t, spans, 1, "expected exactly otelc's span to be exported")
	otelcSpan := spans[0]
	assert.Equal(t, "grpc.testing.TestService/UnaryCall", otelcSpan.Name)
	assert.Equal(t, trace.SpanKindServer, otelcSpan.SpanKind)
	assert.Equal(t, instrumentationName, otelcSpan.InstrumentationScope.Name)
	assert.Equal(t, otelcodes.Error, otelcSpan.Status.Code, "errored RPC should set span status to Error")
	require.Len(t, otelcSpan.Events, 1, "expected one exception event for the recorded error")
	assert.Equal(t, "exception", otelcSpan.Events[0].Name)
	var grpcStatusCode any
	for _, attr := range otelcSpan.Attributes {
		if string(attr.Key) == "rpc.grpc.status_code" {
			grpcStatusCode = attr.Value.AsInt64()
		}
	}
	assert.Equal(t, int64(codes.Internal), grpcStatusCode, "otelc's span must carry the gRPC status code")
	var clientAddress any
	for _, attr := range otelcSpan.Attributes {
		if string(attr.Key) == "client.address" {
			clientAddress = attr.Value.AsString()
		}
	}
	assert.Equal(t, "127.0.0.1", clientAddress, "otelc's span must carry the peer address from OutHeader")

	// The other layer ends its own span and it must stay clean of otelc data.
	otherHandler.HandleRPC(chainedCtx, end)
	spans = exporter.GetSpans()
	require.Len(t, spans, 2, "expected both handlers' spans to be exported")
	for _, s := range spans {
		if s.Name != "other-layer//grpc.testing.TestService/UnaryCall" {
			continue
		}
		assert.Equal(t, otelcodes.Unset, s.Status.Code, "otelc must not set the other span's status")
		assert.Empty(t, s.Events, "otelc must not record events on the other span")
		for _, attr := range s.Attributes {
			assert.NotEqual(t, "rpc.grpc.status_code", string(attr.Key),
				"otelc must not stamp rpc.grpc.status_code onto the other span")
			assert.NotEqual(t, "client.address", string(attr.Key),
				"otelc must not stamp the peer address onto the other span")
		}
	}
}

func TestServerStatsHandler_TagConn(t *testing.T) {
	handler := newServerStatsHandler()

	ctx := t.Context()
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

	ctx := t.Context()

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
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(oldTP)
	})

	// Re-initialize to use new tracer provider
	tracer = tp.Tracer(instrumentationName, trace.WithInstrumentationVersion(runtime.ModuleVersion()))

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

// TestServerStatsHandler_SkippedRPC_DoesNotTouchCallerSpan is the regression test for the
// caller-span corruption bug on the server side. When TagRPC opts out of an OTLP export
// path, HandleRPC must be a complete no-op: it must not stamp gRPC attributes onto, set
// the status of, or end any span that happens to be active on the caller's context.
func TestServerStatsHandler_SkippedRPC_DoesNotTouchCallerSpan(t *testing.T) {
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
	handler := newServerStatsHandler()
	skippedCtx := handler.TagRPC(baseCtx, &stats.RPCTagInfo{
		FullMethodName: "/opentelemetry.proto.collector.trace.v1.TraceService/Export",
	})

	// The returned context must carry no gRPCContext — the skip sentinel.
	require.Nil(t, skippedCtx.Value(gRPCContextKey{}), "TagRPC must not attach gRPCContext for OTLP paths")

	// Drive every HandleRPC branch that previously touched the span directly.
	handler.HandleRPC(skippedCtx, &stats.Begin{BeginTime: time.Now()})
	handler.HandleRPC(skippedCtx, &stats.OutHeader{})
	handler.HandleRPC(skippedCtx, &stats.InPayload{Length: 64})
	handler.HandleRPC(skippedCtx, &stats.OutPayload{Length: 128})
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

// TestServerStatsHandler_NilGRPCContext_IsSafeNoOp pins the invariant that the early
// return in HandleRPC is load-bearing for memory safety, not just correctness. Every
// branch below that guard dereferences gctx without a nil check (gctx.inMessages,
// gctx.metricAttrs), so moving or dropping the guard turns a skipped RPC into a nil
// pointer panic inside the instrumented application rather than a silent no-op.
func TestServerStatsHandler_NilGRPCContext_IsSafeNoOp(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")

	initInstrumentation()

	handler := newServerStatsHandler()
	ctx := t.Context() // no gRPCContext attached, and no span either

	require.Nil(t, ctx.Value(gRPCContextKey{}), "precondition: context carries no gRPCContext")

	require.NotPanics(t, func() {
		handler.HandleRPC(ctx, &stats.Begin{BeginTime: time.Now()})
		handler.HandleRPC(ctx, &stats.OutHeader{})
		handler.HandleRPC(ctx, &stats.InPayload{Length: 64})
		handler.HandleRPC(ctx, &stats.OutPayload{Length: 128})
		handler.HandleRPC(ctx, &stats.End{
			BeginTime: time.Now().Add(-10 * time.Millisecond),
			EndTime:   time.Now(),
		})
	}, "HandleRPC must return before dereferencing a nil gRPCContext")
}

type nonRecordingSpan struct {
	trace.Span
	ended bool
}

func (s *nonRecordingSpan) IsRecording() bool {
	return false
}

func (s *nonRecordingSpan) End(_ ...trace.SpanEndOption) {
	s.ended = true
}

// initInstrumentation is skipped on purpose: the span and gRPCContext are
// injected by hand, and the metric instruments stay nil-guarded zero values.
func TestServerStatsHandler_HandleRPC_NonRecordingSpan(t *testing.T) {
	handler := newServerStatsHandler()

	mockSpan := &nonRecordingSpan{Span: trace.SpanFromContext(t.Context())}
	ctx := context.WithValue(t.Context(), gRPCContextKey{}, &gRPCContext{span: mockSpan})

	handler.HandleRPC(ctx, &stats.End{
		BeginTime: time.Now().Add(-10 * time.Millisecond),
		EndTime:   time.Now(),
	})

	assert.True(t, mockSpan.ended, "span.End() must be called for non-recording spans")
}

func TestServerStatsHandler_UnsampledRPC_NoExport(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")
	initInstrumentation()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.NeverSample()),
	)
	oldTP := otel.GetTracerProvider()
	oldTracer := tracer
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(oldTP)
		tracer = oldTracer
	})
	tracer = tp.Tracer(instrumentationName, trace.WithInstrumentationVersion(runtime.ModuleVersion()))

	handler := newServerStatsHandler()
	ctx := handler.TagRPC(t.Context(), &stats.RPCTagInfo{
		FullMethodName: "/test.Service/Method",
	})
	span := trace.SpanFromContext(ctx)
	require.False(t, span.IsRecording(), "sampler should have marked the span non-recording")

	handler.HandleRPC(ctx, &stats.End{
		BeginTime: time.Now().Add(-50 * time.Millisecond),
		EndTime:   time.Now(),
	})

	require.Empty(t, exporter.GetSpans(), "NeverSample spans are not exported")
}

func TestServerStatsHandler_HandleRPC_UsesStatsEndTime(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "grpc")
	initInstrumentation()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	oldTP := otel.GetTracerProvider()
	oldTracer := tracer
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(oldTP)
		tracer = oldTracer
	})
	tracer = tp.Tracer(instrumentationName, trace.WithInstrumentationVersion(runtime.ModuleVersion()))

	handler := newServerStatsHandler()

	t.Run("explicit EndTime", func(t *testing.T) {
		exporter.Reset()
		ctx := handler.TagRPC(t.Context(), &stats.RPCTagInfo{
			FullMethodName: "/test.Service/Method",
		})

		endTime := time.Now().Add(5 * time.Second).Truncate(time.Microsecond)
		handler.HandleRPC(ctx, &stats.End{
			BeginTime: time.Now(),
			EndTime:   endTime,
		})

		spans := exporter.GetSpans()
		require.Len(t, spans, 1)
		assert.True(t, spans[0].EndTime.Equal(endTime), "span EndTime should match stats.End.EndTime")
	})

	t.Run("zero EndTime", func(t *testing.T) {
		exporter.Reset()
		before := time.Now().Truncate(time.Microsecond)
		ctx := handler.TagRPC(t.Context(), &stats.RPCTagInfo{
			FullMethodName: "/test.Service/Method",
		})

		handler.HandleRPC(ctx, &stats.End{})

		after := time.Now().Add(time.Second)
		spans := exporter.GetSpans()
		require.Len(t, spans, 1)
		assert.False(t, spans[0].EndTime.IsZero(), "span EndTime should not be zero")
		assert.True(
			t,
			!spans[0].EndTime.Before(before) && !spans[0].EndTime.After(after),
			"span EndTime should be set to current time",
		)
	})
}
