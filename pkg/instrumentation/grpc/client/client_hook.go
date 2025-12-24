// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

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
	instrumentationName        = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/grpc"
	instrumentationKey         = "GRPC"
	dialOptionsParamIndex      = 2 // DialContext(ctx, target, opts...)
	newClientOptionsParamIndex = 1 // NewClient(target, opts...)
	otelExporterPrefix         = "grpc-go"
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
	clientDuration        rpcconv.ClientDuration
	clientRequestSize     int64Hist
	clientResponseSize    int64Hist
	clientRequestsPerRPC  rpcconv.ClientRequestsPerRPC
	clientResponsesPerRPC rpcconv.ClientResponsesPerRPC
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
		if err := shared.SetupOTelSDK("go.opentelemetry.io/compile-instrumentation/grpc/client", version); err != nil {
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
		clientDuration, err = rpcconv.NewClientDuration(meter)
		if err != nil {
			logger.Error("failed to create client duration metric", "error", err)
		}

		clientRequestSize, err = rpcconv.NewClientRequestSize(meter)
		if err != nil {
			logger.Error("failed to create client request size metric", "error", err)
		}

		clientResponseSize, err = rpcconv.NewClientResponseSize(meter)
		if err != nil {
			logger.Error("failed to create client response size metric", "error", err)
		}

		clientRequestsPerRPC, err = rpcconv.NewClientRequestsPerRPC(meter)
		if err != nil {
			logger.Error("failed to create client requests per RPC metric", "error", err)
		}

		clientResponsesPerRPC, err = rpcconv.NewClientResponsesPerRPC(meter)
		if err != nil {
			logger.Error("failed to create client responses per RPC metric", "error", err)
		}

		// Start runtime metrics (respects OTEL_GO_ENABLED/DISABLED_INSTRUMENTATIONS)
		if err := shared.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}

		logger.Info("gRPC client instrumentation initialized")
	})
}

// grpcClientEnabler controls whether client instrumentation is enabled
type grpcClientEnabler struct{}

func (g grpcClientEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var clientEnabler = grpcClientEnabler{}

// BeforeNewClient hooks before grpc.NewClient (v1.63+)
func BeforeNewClient(ictx inst.HookContext, target string, opts ...grpc.DialOption) {
	if !clientEnabler.Enable() {
		logger.Debug("gRPC client instrumentation disabled")
		return
	}

	initInstrumentation()

	logger.Debug("BeforeNewClient called", "target", target)

	// Create and inject stats handler
	handler := newClientStatsHandler()
	newOpts := append([]grpc.DialOption{grpc.WithStatsHandler(handler)}, opts...)
	ictx.SetParam(newClientOptionsParamIndex, newOpts)
}

// AfterNewClient hooks after grpc.NewClient
func AfterNewClient(ictx inst.HookContext, conn *grpc.ClientConn, err error) {
	if !clientEnabler.Enable() {
		return
	}
	if err != nil {
		logger.Debug("AfterNewClient called with error", "error", err)
	} else {
		logger.Debug("AfterNewClient called")
	}
}

// BeforeDialContext hooks before grpc.DialContext (v1.44-1.63)
func BeforeDialContext(ictx inst.HookContext, ctx context.Context, target string, opts ...grpc.DialOption) {
	if !clientEnabler.Enable() {
		logger.Debug("gRPC client instrumentation disabled")
		return
	}

	initInstrumentation()

	logger.Debug("BeforeDialContext called", "target", target)

	// Create and inject stats handler
	handler := newClientStatsHandler()
	newOpts := append([]grpc.DialOption{grpc.WithStatsHandler(handler)}, opts...)
	ictx.SetParam(dialOptionsParamIndex, newOpts)
}

// AfterDialContext hooks after grpc.DialContext
func AfterDialContext(ictx inst.HookContext, conn *grpc.ClientConn, err error) {
	if !clientEnabler.Enable() {
		return
	}
	if err != nil {
		logger.Debug("AfterDialContext called with error", "error", err)
	} else {
		logger.Debug("AfterDialContext called")
	}
}

type gRPCContextKey struct{}

type gRPCContext struct {
	inMessages    int64
	outMessages   int64
	metricAttrs   []attribute.KeyValue
	metricAttrSet attribute.Set
}

type clientStatsHandler struct{}

func newClientStatsHandler() stats.Handler {
	return &clientStatsHandler{}
}

// TagRPC is called at the beginning of an RPC to create a context
func (h *clientStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	// Skip instrumentation for OTLP exporter endpoints to prevent infinite recursion
	if grpcsemconv.IsOTELExporterPath(info.FullMethodName) {
		return ctx
	}

	// Parse method name and get attributes
	name, attrs := grpcsemconv.ParseFullMethod(info.FullMethodName)

	// Start span
	ctx, _ = tracer.Start(
		ctx,
		name,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)

	// Inject trace context into outgoing metadata
	ctx = grpcsemconv.Inject(ctx, propagator)

	// Store gRPC context for metrics
	gctx := &gRPCContext{
		metricAttrs:   attrs,
		metricAttrSet: attribute.NewSet(attrs...),
	}

	return context.WithValue(ctx, gRPCContextKey{}, gctx)
}

// HandleRPC processes RPC stats events
func (h *clientStatsHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	span := trace.SpanFromContext(ctx)
	gctx, _ := ctx.Value(gRPCContextKey{}).(*gRPCContext)

	switch rs := rs.(type) {
	case *stats.Begin:
		// RPC started
	case *stats.OutPayload:
		if gctx != nil {
			atomic.AddInt64(&gctx.outMessages, 1)
			if clientRequestSize != nil {
				clientRequestSize.RecordSet(ctx, int64(rs.Length), gctx.metricAttrSet)
			}
		}
	case *stats.InPayload:
		if gctx != nil {
			atomic.AddInt64(&gctx.inMessages, 1)
			if clientResponseSize != nil {
				clientResponseSize.RecordSet(ctx, int64(rs.Length), gctx.metricAttrSet)
			}
		}
	case *stats.OutHeader:
		// Add server address attributes
		if span.IsRecording() {
			if p, ok := peer.FromContext(ctx); ok {
				span.SetAttributes(grpcsemconv.ServerAddrAttrs(p.Addr.String())...)
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
				code, msg := grpcsemconv.ClientStatus(s)
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

			if clientDuration.Inst() != nil {
				clientDuration.Inst().Record(ctx, duration, recordOpts...)
			}
			if clientRequestsPerRPC.Inst() != nil {
				clientRequestsPerRPC.Inst().Record(ctx, atomic.LoadInt64(&gctx.outMessages), recordOpts...)
			}
			if clientResponsesPerRPC.Inst() != nil {
				clientResponsesPerRPC.Inst().Record(ctx, atomic.LoadInt64(&gctx.inMessages), recordOpts...)
			}
		}
	}
}

// TagConn is called when a new connection is established
func (h *clientStatsHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

// HandleConn processes connection stats
func (h *clientStatsHandler) HandleConn(context.Context, stats.ConnStats) {
	// no-op
}
