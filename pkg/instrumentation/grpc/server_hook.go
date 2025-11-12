// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"context"
	"log"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	instapi "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api"
)

var grpcServerInstrumenter = BuildGrpcServerInstrumenter()

// BeforeNewServer is called before the gRPC NewServer() call
// This function injects a StatsHandler into the server options to enable instrumentation
func BeforeNewServer(ictx inst.HookContext, opts ...grpc.ServerOption) {
	if !serverEnabler.Enable() {
		log.Println("[otel-grpc] gRPC server instrumentation is disabled")
		return
	}

	log.Println("[otel-grpc] Injecting StatsHandler for gRPC server instrumentation")

	// Create and inject the stats handler
	handler := newServerStatsHandler()
	newOpts := []grpc.ServerOption{grpc.StatsHandler(handler)}
	newOpts = append(newOpts, opts...)

	// Replace the opts parameter with our modified options
	ictx.SetParam(0, newOpts)

	log.Printf("[otel-grpc] Injected StatsHandler, total options: %d", len(newOpts))
}

// newServerStatsHandler creates a new stats.Handler for server-side instrumentation
func newServerStatsHandler() stats.Handler {
	return &serverStatsHandler{
		propagator: otel.GetTextMapPropagator(),
	}
}

// serverStatsHandler implements stats.Handler for gRPC server instrumentation
type serverStatsHandler struct {
	serverAddr string
	propagator propagation.TextMapPropagator
}

// TagConn attaches connection information to the context
func (h *serverStatsHandler) TagConn(ctx context.Context, info *stats.ConnTagInfo) context.Context {
	// Store the server address for use in RPC instrumentation
	if info.LocalAddr != nil {
		h.serverAddr = info.LocalAddr.String()
		log.Printf("[otel-grpc] Server connection tagged: %s", h.serverAddr)
	}
	return ctx
}

// HandleConn processes connection stats (no-op for now)
func (h *serverStatsHandler) HandleConn(ctx context.Context, s stats.ConnStats) {
	// We don't need to handle connection-level stats for basic instrumentation
}

// TagRPC attaches RPC information and starts instrumentation
func (h *serverStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	log.Printf("[otel-grpc] TagRPC called for method: %s", info.FullMethodName)

	// Extract context from incoming metadata
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		md = metadata.MD{}
	}

	// Extract trace context from metadata
	carrier := &grpcMetadataCarrier{metadata: &md}
	ctx = h.propagator.Extract(ctx, carrier)

	// Start instrumentation
	request := grpcRequest{
		methodName:    info.FullMethodName,
		serverAddress: h.serverAddr,
		propagators:   carrier,
	}

	newCtx := grpcServerInstrumenter.Start(ctx, request)

	// Store RPC context for use in HandleRPC
	rpcCtx := &gRPCContext{
		methodName: info.FullMethodName,
	}
	return context.WithValue(newCtx, gRPCContextKey{}, rpcCtx)
}

// HandleRPC processes RPC-level stats
func (h *serverStatsHandler) HandleRPC(ctx context.Context, s stats.RPCStats) {
	switch rs := s.(type) {
	case *stats.End:
		// RPC has ended, finish the span
		log.Printf("[otel-grpc] HandleRPC End event, error: %v", rs.Error)

		// Get the stored RPC context
		rpcCtx, _ := ctx.Value(gRPCContextKey{}).(*gRPCContext)

		var statusCode int
		var err error

		if rs.Error != nil {
			// Extract gRPC status code from error
			st, ok := status.FromError(rs.Error)
			if ok {
				statusCode = int(st.Code())
			} else {
				statusCode = int(codes.Unknown)
			}
			err = rs.Error
		} else {
			statusCode = 0 // OK
		}

		request := grpcRequest{
			methodName:    "",
			serverAddress: h.serverAddr,
		}
		if rpcCtx != nil {
			request.methodName = rpcCtx.methodName
		}

		response := grpcResponse{
			statusCode: statusCode,
		}

		invocation := instapi.Invocation[grpcRequest, grpcResponse]{
			Request:  request,
			Response: response,
			Err:      err,
		}

		grpcServerInstrumenter.End(ctx, invocation)
		log.Printf("[otel-grpc] Span ended for method: %s, status: %d", request.methodName, statusCode)

	case *stats.InPayload:
		// Could add message events here if needed
		log.Printf("[otel-grpc] InPayload: %d bytes", rs.Length)

	case *stats.OutPayload:
		// Could add message events here if needed
		log.Printf("[otel-grpc] OutPayload: %d bytes", rs.Length)

	default:
		// Other stats events (Begin, InHeader, OutHeader, OutTrailer) are ignored
	}
}
