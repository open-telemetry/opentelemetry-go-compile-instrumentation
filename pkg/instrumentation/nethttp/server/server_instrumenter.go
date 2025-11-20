// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/instrumentation"

	instrumenter "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api"
	httpconv "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/http"
	netconv "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/net"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/otelsetup"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
)

const (
	instrumentationName    = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp"
	instrumentationVersion = "0.1.0"
)

// netHttpServerEnabler controls whether server instrumentation is enabled
type netHttpServerEnabler struct{}

func (n netHttpServerEnabler) Enable() bool {
	return shared.IsInstrumentationEnabled("NETHTTP")
}

var serverEnabler = netHttpServerEnabler{}

// BuildNetHttpServerOtelInstrumenter builds an instrumenter for HTTP server operations
func BuildNetHttpServerOtelInstrumenter() *instrumenter.PropagatingFromUpstreamInstrumenter[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse] {
	builder := &instrumenter.Builder[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse]{}
	serverGetter := &netHttpServerAttrsGetter{}

	// Create HTTP common attributes extractor
	commonExtractor := httpconv.HTTPCommonAttrsExtractor[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse, httpconv.HTTPServerAttrsGetter[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse]]{
		HTTPGetter: serverGetter,
	}

	// Create network attributes extractor
	networkExtractor := netconv.CreateNetworkAttributesExtractor(serverGetter)

	// Create URL attributes extractor
	urlExtractor := &netconv.URLAttrsExtractor[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse, netconv.URLAttrsGetter[*nethttp.NetHttpRequest]]{
		Getter: serverGetter,
	}

	// Create HTTP server attributes extractor
	httpServerExtractor := &httpconv.HTTPServerAttrsExtractor[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse, httpconv.HTTPServerAttrsGetter[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse]]{
		Base: commonExtractor,
	}

	// Create metrics registry and HTTP server metrics
	metricsRegistry := httpconv.NewMetricsRegistry(
		otelsetup.GetLogger(),
		otelsetup.GetMeterProvider().Meter(instrumentationName),
	)
	serverMetrics, err := metricsRegistry.NewHTTPServerMetric(instrumentationName)
	if err != nil {
		otelsetup.GetLogger().Error("failed to create HTTP server metrics", "error", err)
	}

	base := builder.Init().
		SetInstrumentEnabler(serverEnabler).
		SetSpanStatusExtractor(httpconv.HTTPServerSpanStatusExtractor[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse]{Getter: serverGetter}).
		SetSpanNameExtractor(&httpconv.HTTPServerSpanNameExtractor[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse]{Getter: serverGetter}).
		SetSpanKindExtractor(&instrumenter.AlwaysServerExtractor[*nethttp.NetHttpRequest]{}).
		SetInstrumentationScope(instrumentation.Scope{
			Name:    instrumentationName,
			Version: instrumentationVersion,
		}).
		AddAttributesExtractor(httpServerExtractor).
		AddAttributesExtractor(&networkExtractor).
		AddAttributesExtractor(urlExtractor)

	// Add metrics if successfully created
	if serverMetrics != nil {
		base.AddOperationListeners(serverMetrics)
	}

	return base.BuildPropagatingFromUpstreamInstrumenter(
		func(req *nethttp.NetHttpRequest) propagation.TextMapCarrier {
			if req.Header() == nil {
				return nil
			}
			return propagation.HeaderCarrier(req.Header())
		},
		otel.GetTextMapPropagator(),
	)
}
