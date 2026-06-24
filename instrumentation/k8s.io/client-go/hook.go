// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package k8s_client_go

import (
	"context"
	"runtime/debug"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/client-go/tools/cache"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/k8s.io/client-go/semconv"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
)

const (
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/k8s.io/client-go"
	instrumentationKey  = "K8S_CLIENT_GO"
)

var (
	logger   = runtime.Logger()
	tracer   trace.Tracer
	initOnce sync.Once
)

// k8SClientGoEnabler controls whether library instrumentation is enabled
type k8SClientGoEnabler struct{}

func (g k8SClientGoEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var k8SEnabler = k8SClientGoEnabler{}

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
		if err := runtime.SetupOTelSDK(
			"go.opentelemetry.io/compile-instrumentation/k8s.io/client-go",
			version,
		); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(version),
		)

		// Start runtime metrics (respects OTEL_GO_ENABLED/DISABLED_INSTRUMENTATIONS)
		if err := runtime.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}

		logger.Info("K8S client-go instrumentation initialized")
	})
}

func beforeProcessDeltas(
	ictx hook.HookContext,
	handler cache.ResourceEventHandler,
	_ cache.Store,
	deltas cache.Deltas,
	isInInitialList bool,
) {
	if !k8SEnabler.Enable() {
		logger.Debug("K8S client-go instrumentation disabled")
		return
	}
	initInstrumentation()

	objsInfo := semconv.K8SObjectsInfo{
		Count:           len(deltas),
		IsInInitialList: isInInitialList,
	}
	attrs := semconv.K8SObjectsInfoTraceAttrs(objsInfo)

	spanName := "k8s.informer.objects.process"
	ctx, span := tracer.Start(context.Background(),
		spanName,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)

	ictx.SetParam(0, newK8SOtelEventHandler(handler, ctx))
	ictx.SetKeyData("span", span)
}

func afterProcessDeltas(ictx hook.HookContext, err error) {
	if !k8SEnabler.Enable() {
		logger.Debug("K8S client-go instrumentation disabled")
		return
	}

	span, ok := ictx.GetKeyData("span").(trace.Span)
	if !ok || span == nil {
		logger.Debug("afterProcessDeltas: no span from before hook")
		return
	}
	defer span.End()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

func beforeProcessDeltasInBatch(
	ictx hook.HookContext,
	handler cache.ResourceEventHandler,
	_ cache.Store,
	deltas []cache.Delta,
	isInInitialList bool,
) {
	if !k8SEnabler.Enable() {
		logger.Debug("K8S client-go instrumentation disabled")
		return
	}
	initInstrumentation()

	objsInfo := semconv.K8SObjectsInfo{
		Count:           len(deltas),
		IsInInitialList: isInInitialList,
	}
	attrs := semconv.K8SObjectsInfoTraceAttrs(objsInfo)

	spanName := "k8s.informer.objects.process"
	ctx, span := tracer.Start(context.Background(),
		spanName,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)

	ictx.SetParam(0, newK8SOtelEventHandler(handler, ctx))
	ictx.SetKeyData("span", span)
}

func afterProcessDeltasInBatch(ictx hook.HookContext, err error) {
	if !k8SEnabler.Enable() {
		logger.Debug("K8S client-go instrumentation disabled")
		return
	}

	span, ok := ictx.GetKeyData("span").(trace.Span)
	if !ok || span == nil {
		logger.Debug("afterProcessDeltasInBatch: no span from before hook")
		return
	}
	defer span.End()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}
