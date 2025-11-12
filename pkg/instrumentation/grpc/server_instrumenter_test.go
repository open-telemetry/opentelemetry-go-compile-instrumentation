// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc/metadata"

	instapi "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api"
)

func TestBuildGrpcServerInstrumenter(t *testing.T) {
	instrumenter := BuildGrpcServerInstrumenter()
	require.NotNil(t, instrumenter)
}

func TestGrpcServerInstrumentation_Start(t *testing.T) {
	// Setup trace provider
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)

	instrumenter := BuildGrpcServerInstrumenter()

	md := metadata.MD{
		"test-key": []string{"test-value"},
	}
	request := grpcRequest{
		methodName:    "/helloworld.Greeter/SayHello",
		serverAddress: "localhost:50051",
		propagators:   &grpcMetadataCarrier{metadata: &md},
	}

	ctx := context.Background()
	newCtx := instrumenter.Start(ctx, request)
	require.NotNil(t, newCtx)
	require.NotEqual(t, ctx, newCtx)

	// End the span
	response := grpcResponse{statusCode: 0}
	invocation := instapi.Invocation[grpcRequest, grpcResponse]{
		Request:  request,
		Response: response,
		Err:      nil,
	}
	instrumenter.End(newCtx, invocation)

	// Verify spans were created
	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	span := spans[0]
	require.Equal(t, "/helloworld.Greeter/SayHello", span.Name)

	// Verify attributes
	attrs := span.Attributes
	require.Contains(t, attrs, findAttr("rpc.system", "grpc"))
	require.Contains(t, attrs, findAttr("rpc.service", "/helloworld.Greeter"))
	require.Contains(t, attrs, findAttr("rpc.method", "SayHello"))
	require.Contains(t, attrs, findAttr("server.address", "localhost:50051"))
}

func TestGrpcServerInstrumentation_WithPropagation(t *testing.T) {
	// Setup trace provider
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)

	// Setup propagator
	otel.SetTextMapPropagator(propagation.TraceContext{})

	instrumenter := BuildGrpcServerInstrumenter()

	// Create a parent span to propagate
	ctx := context.Background()
	tracer := tp.Tracer("test")
	parentCtx, parentSpan := tracer.Start(ctx, "parent")
	defer parentSpan.End()

	// Extract context into metadata
	md := metadata.MD{}
	carrier := &grpcMetadataCarrier{metadata: &md}
	otel.GetTextMapPropagator().Inject(parentCtx, carrier)

	request := grpcRequest{
		methodName:    "/helloworld.Greeter/SayHello",
		serverAddress: "localhost:50051",
		propagators:   carrier,
	}

	// Start instrumentation (should extract parent context)
	newCtx := instrumenter.Start(context.Background(), request)
	require.NotNil(t, newCtx)

	// End the span
	response := grpcResponse{statusCode: 0}
	invocation := instapi.Invocation[grpcRequest, grpcResponse]{
		Request:  request,
		Response: response,
		Err:      nil,
	}
	instrumenter.End(newCtx, invocation)

	// Verify spans
	spans := exporter.GetSpans()
	require.GreaterOrEqual(t, len(spans), 2) // Parent and server spans

	// Find the server span
	var serverSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "/helloworld.Greeter/SayHello" {
			serverSpan = &spans[i]
			break
		}
	}
	require.NotNil(t, serverSpan)

	// Verify the server span has the correct parent
	require.Equal(t, parentSpan.SpanContext().TraceID(), serverSpan.SpanContext.TraceID())
}

func TestGrpcServerInstrumentation_Disabled(t *testing.T) {
	// Disable instrumentation
	os.Setenv("OTEL_INSTRUMENTATION_GRPC_ENABLED", "false")
	defer os.Unsetenv("OTEL_INSTRUMENTATION_GRPC_ENABLED")

	// Setup trace provider
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)

	// Need to rebuild instrumenter after env var change
	instrumenter := BuildGrpcServerInstrumenter()

	md := metadata.MD{}
	request := grpcRequest{
		methodName:    "/helloworld.Greeter/SayHello",
		serverAddress: "localhost:50051",
		propagators:   &grpcMetadataCarrier{metadata: &md},
	}

	ctx := context.Background()
	newCtx := instrumenter.Start(ctx, request)

	response := grpcResponse{statusCode: 0}
	invocation := instapi.Invocation[grpcRequest, grpcResponse]{
		Request:  request,
		Response: response,
		Err:      nil,
	}
	instrumenter.End(newCtx, invocation)

	// Verify no spans were created when disabled
	spans := exporter.GetSpans()
	require.Len(t, spans, 0)
}

// Helper function to find an attribute by key and value
func findAttr(key, value string) func(tracetest.SpanStub) bool {
	return func(s tracetest.SpanStub) bool {
		for _, attr := range s.Attributes {
			if string(attr.Key) == key && attr.Value.AsString() == value {
				return true
			}
		}
		return false
	}
}
