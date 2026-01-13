//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"testing"
	"time"

	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

func TestGrpc(t *testing.T) {
	f := app.NewE2EFixture(t)

	f.BuildApp("grpcserver")
	f.StartApp("grpcserver")
	time.Sleep(time.Second)

	f.BuildApp("grpcclient")
	f.RunApp("grpcclient", "-name", "OpenTelemetry")
	f.RunApp("grpcclient", "-stream")

	f.RequireTraceCount(2)    // unary + stream
	f.RequireSpansPerTrace(2) // client + server per trace

	grpcClientSpan := app.RequireSpan(t, f.Traces(),
		app.IsClient,
		app.HasAttribute(string(semconv.RPCSystemKey), "grpc"),
	)
	app.RequireGRPCClientSemconv(t, grpcClientSpan, "::1")

	grpcServerSpan := app.RequireSpan(t, f.Traces(),
		app.IsServer,
		app.HasAttribute(string(semconv.RPCSystemKey), "grpc"),
	)
	app.RequireGRPCServerSemconv(t, grpcServerSpan)
}
