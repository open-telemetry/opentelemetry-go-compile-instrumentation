// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nethttp

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/instrumentation"

	instrumenter "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api"
	httpconv "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/http"
	netconv "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/net"
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
func BuildNetHttpServerOtelInstrumenter() *instrumenter.PropagatingFromUpstreamInstrumenter[*netHttpRequest, *netHttpResponse] {
	builder := &instrumenter.Builder[*netHttpRequest, *netHttpResponse]{}
	serverGetter := &netHttpServerAttrsGetter{}

	// Create HTTP common attributes extractor
	commonExtractor := httpconv.HTTPCommonAttrsExtractor[*netHttpRequest, *netHttpResponse, httpconv.HTTPServerAttrsGetter[*netHttpRequest, *netHttpResponse]]{
		HTTPGetter: serverGetter,
	}

	// Create network attributes extractor
	networkExtractor := netconv.CreateNetworkAttributesExtractor(serverGetter)

	// Create URL attributes extractor
	urlExtractor := &netconv.URLAttrsExtractor[*netHttpRequest, *netHttpResponse, netconv.URLAttrsGetter[*netHttpRequest]]{
		Getter: serverGetter,
	}

	// Create HTTP server attributes extractor
	httpServerExtractor := &httpconv.HTTPServerAttrsExtractor[*netHttpRequest, *netHttpResponse, httpconv.HTTPServerAttrsGetter[*netHttpRequest, *netHttpResponse]]{
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
		SetSpanStatusExtractor(httpconv.HTTPServerSpanStatusExtractor[*netHttpRequest, *netHttpResponse]{Getter: serverGetter}).
		SetSpanNameExtractor(&httpconv.HTTPServerSpanNameExtractor[*netHttpRequest, *netHttpResponse]{Getter: serverGetter}).
		SetSpanKindExtractor(&instrumenter.AlwaysServerExtractor[*netHttpRequest]{}).
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
		func(req *netHttpRequest) propagation.TextMapCarrier {
			if req.header == nil {
				return nil
			}
			return propagation.HeaderCarrier(req.header)
		},
		otel.GetTextMapPropagator(),
	)
}
