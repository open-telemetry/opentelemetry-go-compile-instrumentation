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

// netHttpClientEnabler controls whether client instrumentation is enabled
type netHttpClientEnabler struct{}

func (n netHttpClientEnabler) Enable() bool {
	return shared.IsInstrumentationEnabled("NETHTTP")
}

var clientEnabler = netHttpClientEnabler{}

// BuildNetHttpClientOtelInstrumenter builds an instrumenter for HTTP client operations
func BuildNetHttpClientOtelInstrumenter() *instrumenter.PropagatingToDownstreamInstrumenter[*netHttpRequest, *netHttpResponse] {
	builder := &instrumenter.Builder[*netHttpRequest, *netHttpResponse]{}
	clientGetter := &netHttpClientAttrsGetter{}

	// Create HTTP common attributes extractor
	commonExtractor := httpconv.HTTPCommonAttrsExtractor[*netHttpRequest, *netHttpResponse, httpconv.HTTPClientAttrsGetter[*netHttpRequest, *netHttpResponse]]{
		HTTPGetter: clientGetter,
	}

	// Create network attributes extractor
	networkExtractor := netconv.CreateNetworkAttributesExtractor[*netHttpRequest, *netHttpResponse](clientGetter)

	// Create HTTP client attributes extractor
	httpClientExtractor := &httpconv.HTTPClientAttrsExtractor[*netHttpRequest, *netHttpResponse, httpconv.HTTPClientAttrsGetter[*netHttpRequest, *netHttpResponse]]{
		Base: commonExtractor,
	}

	// Create metrics registry and HTTP client metrics
	metricsRegistry := httpconv.NewMetricsRegistry(otelsetup.GetLogger(), otelsetup.GetMeterProvider().Meter(instrumentationName))
	clientMetrics, err := metricsRegistry.NewHTTPClientMetric(instrumentationName)
	if err != nil {
		otelsetup.GetLogger().Error("failed to create HTTP client metrics", "error", err)
	}

	base := builder.Init().
		SetInstrumentEnabler(clientEnabler).
		SetSpanStatusExtractor(httpconv.HTTPClientSpanStatusExtractor[*netHttpRequest, *netHttpResponse]{Getter: clientGetter}).
		SetSpanNameExtractor(&httpconv.HTTPClientSpanNameExtractor[*netHttpRequest, *netHttpResponse]{Getter: clientGetter}).
		SetSpanKindExtractor(&instrumenter.AlwaysClientExtractor[*netHttpRequest]{}).
		SetInstrumentationScope(instrumentation.Scope{
			Name:    instrumentationName,
			Version: instrumentationVersion,
		}).
		AddAttributesExtractor(httpClientExtractor).
		AddAttributesExtractor(&networkExtractor)

	// Add metrics if successfully created
	if clientMetrics != nil {
		base.AddOperationListeners(clientMetrics)
	}

	return base.BuildPropagatingToDownstreamInstrumenter(
		func(req *netHttpRequest) propagation.TextMapCarrier {
			if req.header == nil {
				return nil
			}
			return propagation.HeaderCarrier(req.header)
		},
		otel.GetTextMapPropagator(),
	)
}
