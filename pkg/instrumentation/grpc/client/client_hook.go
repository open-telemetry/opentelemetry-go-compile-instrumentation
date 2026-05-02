// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"os"
	"runtime/debug"
	"strings"
	"sync"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	grpcsemconv "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/grpc/semconv"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
)

const (
	instrumentationKey         = "GRPC"
	dialOptionsParamIndex      = 2 // DialContext(ctx, target, opts...)
	newClientOptionsParamIndex = 1 // NewClient(target, opts...)
)

var (
	logger   = shared.Logger()
	initOnce sync.Once
)

func moduleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}

	return "dev"
}

func initInstrumentation() {
	initOnce.Do(func() {
		if err := shared.SetupOTelSDK(
			"go.opentelemetry.io/compile-instrumentation/grpc/client",
			moduleVersion(),
		); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}

		if err := shared.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}

		logger.Info("gRPC client instrumentation initialized")
	})
}

type grpcClientEnabler struct{}

func (g grpcClientEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var clientEnabler = grpcClientEnabler{}

func BeforeNewClient(ictx inst.HookContext, target string, opts ...grpc.DialOption) {
	if !clientEnabler.Enable() {
		logger.Debug("gRPC client instrumentation disabled")
		return
	}

	if isOTLPExporterTarget(target) {
		logger.Debug("Skipping instrumentation for OTLP exporter endpoint", "target", target)
		return
	}

	initInstrumentation()

	logger.Debug("BeforeNewClient called", "target", target)

	newOpts := append([]grpc.DialOption{grpc.WithStatsHandler(newClientStatsHandler())}, opts...)
	ictx.SetParam(newClientOptionsParamIndex, newOpts)
}

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

func BeforeDialContext(ictx inst.HookContext, ctx context.Context, target string, opts ...grpc.DialOption) {
	if !clientEnabler.Enable() {
		logger.Debug("gRPC client instrumentation disabled")
		return
	}

	if isOTLPExporterTarget(target) {
		logger.Debug("Skipping instrumentation for OTLP exporter endpoint", "target", target)
		return
	}

	initInstrumentation()

	logger.Debug("BeforeDialContext called", "target", target)

	newOpts := append([]grpc.DialOption{grpc.WithStatsHandler(newClientStatsHandler())}, opts...)
	ictx.SetParam(dialOptionsParamIndex, newOpts)
}

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

func newClientStatsHandler() stats.Handler {
	return otelgrpc.NewClientHandler(otelgrpc.WithFilter(recordRPC))
}

func recordRPC(info *stats.RPCTagInfo) bool {
	return info == nil || !grpcsemconv.IsOTELExporterPath(info.FullMethodName)
}

func isOTLPExporterTarget(target string) bool {
	if target == "" {
		return false
	}

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	}

	return endpoint != "" && strings.Contains(endpoint, target)
}
