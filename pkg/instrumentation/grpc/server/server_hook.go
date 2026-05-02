// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"runtime/debug"
	"sync"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	grpcsemconv "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/grpc/semconv"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
)

const (
	instrumentationKey = "GRPC"
	optionsParamIndex  = 0
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
			"go.opentelemetry.io/compile-instrumentation/grpc/server",
			moduleVersion(),
		); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}

		if err := shared.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}

		logger.Info("gRPC server instrumentation initialized")
	})
}

type grpcServerEnabler struct{}

func (g grpcServerEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var serverEnabler = grpcServerEnabler{}

func BeforeNewServer(ictx inst.HookContext, opts ...grpc.ServerOption) {
	if !serverEnabler.Enable() {
		logger.Debug("gRPC server instrumentation disabled")
		return
	}

	initInstrumentation()

	logger.Debug("BeforeNewServer called")

	newOpts := append([]grpc.ServerOption{grpc.StatsHandler(newServerStatsHandler())}, opts...)
	ictx.SetParam(optionsParamIndex, newOpts)
}

func AfterNewServer(ictx inst.HookContext, server *grpc.Server) {
	if !serverEnabler.Enable() {
		return
	}
	logger.Debug("AfterNewServer called")
}

func newServerStatsHandler() stats.Handler {
	return otelgrpc.NewServerHandler(otelgrpc.WithFilter(recordRPC))
}

func recordRPC(info *stats.RPCTagInfo) bool {
	return info == nil || !grpcsemconv.IsOTELExporterPath(info.FullMethodName)
}
