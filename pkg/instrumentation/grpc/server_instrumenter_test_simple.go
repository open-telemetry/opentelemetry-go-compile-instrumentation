// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc/metadata"

	instapi "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api"
)

func TestGrpcServerBasic(t *testing.T) {
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

	// Verify key attributes are present
	hasSystem := false
	hasService := false
	hasMethod := false
	for _, attr := range span.Attributes {
		if string(attr.Key) == "rpc.system" && attr.Value.AsString() == "grpc" {
			hasSystem = true
		}
		if string(attr.Key) == "rpc.service" && attr.Value.AsString() == "/helloworld.Greeter" {
			hasService = true
		}
		if string(attr.Key) == "rpc.method" && attr.Value.AsString() == "SayHello" {
			hasMethod = true
		}
	}

	require.True(t, hasSystem, "span should have rpc.system attribute")
	require.True(t, hasService, "span should have rpc.service attribute")
	require.True(t, hasMethod, "span should have rpc.method attribute")
}
