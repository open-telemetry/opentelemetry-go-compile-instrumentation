// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/semconv/v1.37.0/rpcconv"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	grpcsemconv "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/grpc/semconv"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
)

const (
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/grpc"
	instrumentationKey  = "GRPC"
	optionsParamIndex   = 0
)

type int64Hist interface {
	RecordSet(context.Context, int64, attribute.Set)
}

var (
	logger     = shared.Logger()
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	meter      metric.Meter
	initOnce   sync.Once

	// Metrics
	serverDuration        rpcconv.ServerDuration
	serverRequestSize     int64Hist
	serverResponseSize    int64Hist
	serverRequestsPerRPC  rpcconv.ServerRequestsPerRPC
	serverResponsesPerRPC rpcconv.ServerResponsesPerRPC
)

// moduleVersion extracts the version from the Go module system.
// Falls back to "dev" if version cannot be determined.
func moduleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	// Return the main module version
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}

	return "dev"
}

func initInstrumentation() {
	initOnce.Do(func() {
		version := moduleVersion()
		if err := shared.SetupOTelSDK("go.opentelemetry.io/compile-instrumentation/grpc/server", version); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(version),
		)
		propagator = otel.GetTextMapPropagator()
		meter = otel.GetMeterProvider().Meter(
			instrumentationName,
			metric.WithInstrumentationVersion(version),
			metric.WithSchemaURL(semconv.SchemaURL),
		)

		var err error
		serverDuration, err = rpcconv.NewServerDuration(meter)
		if err != nil {
			logger.Error("failed to create server duration metric", "error", err)
		}

		serverRequestSize, err = rpcconv.NewServerRequestSize(meter)
		if err != nil {
			logger.Error("failed to create server request size metric", "error", err)
		}

		serverResponseSize, err = rpcconv.NewServerResponseSize(meter)
		if err != nil {
			logger.Error("failed to create server response size metric", "error", err)
		}

		serverRequestsPerRPC, err = rpcconv.NewServerRequestsPerRPC(meter)
		if err != nil {
			logger.Error("failed to create server requests per RPC metric", "error", err)
		}

		serverResponsesPerRPC, err = rpcconv.NewServerResponsesPerRPC(meter)
		if err != nil {
			logger.Error("failed to create server responses per RPC metric", "error", err)
		}

		// Start runtime metrics (respects OTEL_GO_ENABLED/DISABLED_INSTRUMENTATIONS)
		if err := shared.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}

		logger.Info("gRPC server instrumentation initialized")
	})
}

// grpcServerEnabler controls whether server instrumentation is enabled
type grpcServerEnabler struct{}

func (g grpcServerEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var serverEnabler = grpcServerEnabler{}

// BeforeNewServer hooks before grpc.NewServer to inject stats handler
func BeforeNewServer(ictx inst.HookContext, opts []grpc.ServerOption) {
	if !serverEnabler.Enable() {
		logger.Debug("gRPC server instrumentation disabled")
		return
	}

	initInstrumentation()

	logger.Debug("BeforeNewServer called")

	// Create and inject stats handler
	handler := newServerStatsHandler()
	newOpts := append([]grpc.ServerOption{grpc.StatsHandler(handler)}, opts...)
	ictx.SetParam(optionsParamIndex, newOpts)
}

// AfterNewServer hooks after grpc.NewServer
func AfterNewServer(ictx inst.HookContext, server *grpc.Server) {
	if !serverEnabler.Enable() {
		return
	}
	logger.Debug("AfterNewServer called")
}

type gRPCContextKey struct{}

type gRPCContext struct {
	inMessages    int64
	outMessages   int64
	metricAttrs   []attribute.KeyValue
	metricAttrSet attribute.Set
}

type serverStatsHandler struct{}

func newServerStatsHandler() stats.Handler {
	return &serverStatsHandler{}
}

// TagRPC is called at the beginning of an RPC to create a context
func (h *serverStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	// Skip instrumentation for OTLP exporter endpoints to prevent infinite recursion
	if grpcsemconv.IsOTELExporterPath(info.FullMethodName) {
		return ctx
	}

	// Extract trace context from incoming metadata
	ctx = grpcsemconv.Extract(ctx, propagator)

	// Parse method name and get attributes
	name, attrs := grpcsemconv.ParseFullMethod(info.FullMethodName)

	// Start span
	ctx, _ = tracer.Start(
		trace.ContextWithRemoteSpanContext(ctx, trace.SpanContextFromContext(ctx)),
		name,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	)

	// Store gRPC context for metrics
	gctx := &gRPCContext{
		metricAttrs:   attrs,
		metricAttrSet: attribute.NewSet(attrs...),
	}

	return context.WithValue(ctx, gRPCContextKey{}, gctx)
}

// HandleRPC processes RPC stats events
func (h *serverStatsHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	span := trace.SpanFromContext(ctx)
	gctx, _ := ctx.Value(gRPCContextKey{}).(*gRPCContext)

	switch rs := rs.(type) {
	case *stats.Begin:
		// RPC started
	case *stats.InPayload:
		if gctx != nil {
			atomic.AddInt64(&gctx.inMessages, 1)
			if serverRequestSize != nil {
				serverRequestSize.RecordSet(ctx, int64(rs.Length), gctx.metricAttrSet)
			}
		}
	case *stats.OutPayload:
		if gctx != nil {
			atomic.AddInt64(&gctx.outMessages, 1)
			if serverResponseSize != nil {
				serverResponseSize.RecordSet(ctx, int64(rs.Length), gctx.metricAttrSet)
			}
		}
	case *stats.OutHeader:
		// Add peer address attributes
		if span.IsRecording() {
			if p, ok := peer.FromContext(ctx); ok {
				span.SetAttributes(grpcsemconv.ClientAddrAttrs(p.Addr.String())...)
			}
		}
	case *stats.End:
		// End span
		var s *status.Status
		var statusAttr attribute.KeyValue
		if rs.Error != nil {
			s, _ = status.FromError(rs.Error)
			statusAttr = grpcsemconv.GRPCStatusCodeAttr(int(s.Code()))
		} else {
			s = status.New(0, "") // OK
			statusAttr = grpcsemconv.GRPCStatusCodeAttr(0)
		}

		if span.IsRecording() {
			if s != nil {
				code, msg := grpcsemconv.ServerStatus(s)
				span.SetStatus(code, msg)
			}
			span.SetAttributes(statusAttr)
			span.End()
		}

		// Record metrics
		if gctx != nil {
			metricAttrs := make([]attribute.KeyValue, 0, len(gctx.metricAttrs)+1)
			metricAttrs = append(metricAttrs, gctx.metricAttrs...)
			metricAttrs = append(metricAttrs, statusAttr)
			recordOpts := []metric.RecordOption{metric.WithAttributeSet(attribute.NewSet(metricAttrs...))}

			// Use floating point division for higher precision (instead of Milliseconds method)
			duration := float64(rs.EndTime.Sub(rs.BeginTime)) / float64(time.Millisecond)

			if serverDuration.Inst() != nil {
				serverDuration.Inst().Record(ctx, duration, recordOpts...)
			}
			if serverRequestsPerRPC.Inst() != nil {
				serverRequestsPerRPC.Inst().Record(ctx, atomic.LoadInt64(&gctx.inMessages), recordOpts...)
			}
			if serverResponsesPerRPC.Inst() != nil {
				serverResponsesPerRPC.Inst().Record(ctx, atomic.LoadInt64(&gctx.outMessages), recordOpts...)
			}
		}
	}
}

// TagConn is called when a new connection is established
func (h *serverStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

// HandleConn processes connection stats
func (h *serverStatsHandler) HandleConn(context.Context, stats.ConnStats) {
	// no-op
}
