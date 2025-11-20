// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/status"

	instrumenter "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api"
	rpcconv "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/rpc"
)

const (
	instrumentationName    = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/grpc"
	instrumentationVersion = "0.1.0"
)

// grpcServerEnabler controls whether server instrumentation is enabled
type grpcServerEnabler struct {
	enabled bool
}

func (g grpcServerEnabler) Enable() bool {
	return g.enabled
}

// grpcServerEnabler is enabled by default unless OTEL_INSTRUMENTATION_GRPC_ENABLED is set to "false"
var serverEnabler = grpcServerEnabler{os.Getenv("OTEL_INSTRUMENTATION_GRPC_ENABLED") != "false"}

// grpcStatusCodeExtractor extracts the span status from gRPC responses and errors
type grpcStatusCodeExtractor struct{}

func (g grpcStatusCodeExtractor) Extract(span trace.Span, request grpcRequest, response grpcResponse, err error) {
	if err != nil {
		span.RecordError(err)
		// Try to extract gRPC status
		if st, ok := status.FromError(err); ok {
			span.SetStatus(codes.Error, st.Message())
		} else {
			span.SetStatus(codes.Error, err.Error())
		}
		return
	}

	// Check status code from response
	statusCode := response.statusCode
	if statusCode != 0 {
		span.SetStatus(codes.Error, fmt.Sprintf("gRPC status code %d", statusCode))
	}
}

// BuildGrpcServerInstrumenter builds an instrumenter for gRPC server operations
func BuildGrpcServerInstrumenter() *instrumenter.PropagatingFromUpstreamInstrumenter[grpcRequest, grpcResponse] {
	builder := &instrumenter.Builder[grpcRequest, grpcResponse]{}
	serverGetter := grpcServerAttrsGetter{}

	// Create RPC attributes extractor
	rpcExtractor := &rpcconv.ServerRpcAttrsExtractor[grpcRequest, grpcResponse, grpcServerAttrsGetter]{
		Base: rpcconv.RpcAttrsExtractor[grpcRequest, grpcResponse, grpcServerAttrsGetter]{
			Getter: serverGetter,
		},
	}

	// Create RPC span name extractor
	spanNameExtractor := &rpcconv.RpcSpanNameExtractor[grpcRequest]{
		Getter: serverGetter,
	}

	return builder.Init().
		SetInstrumentEnabler(serverEnabler).
		SetSpanStatusExtractor(&grpcStatusCodeExtractor{}).
		SetSpanNameExtractor(spanNameExtractor).
		SetSpanKindExtractor(&instrumenter.AlwaysServerExtractor[grpcRequest]{}).
		AddOperationListeners(rpcconv.RpcServerMetrics("grpc.server")).
		SetInstrumentationScope(instrumentation.Scope{
			Name:    instrumentationName,
			Version: instrumentationVersion,
		}).
		AddAttributesExtractor(rpcExtractor).
		BuildPropagatingFromUpstreamInstrumenter(
			func(req grpcRequest) propagation.TextMapCarrier {
				if req.propagators == nil {
					return nil
				}
				return req.propagators
			},
			otel.GetTextMapPropagator(),
		)
}
