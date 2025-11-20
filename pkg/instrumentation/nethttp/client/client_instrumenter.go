// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

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

// netHttpClientEnabler controls whether client instrumentation is enabled
type netHttpClientEnabler struct{}

func (n netHttpClientEnabler) Enable() bool {
	return shared.IsInstrumentationEnabled("NETHTTP")
}

var clientEnabler = netHttpClientEnabler{}

// BuildNetHttpClientOtelInstrumenter builds an instrumenter for HTTP client operations
func BuildNetHttpClientOtelInstrumenter() *instrumenter.PropagatingToDownstreamInstrumenter[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse] {
	builder := &instrumenter.Builder[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse]{}
	clientGetter := &netHttpClientAttrsGetter{}

	// Create HTTP common attributes extractor
	commonExtractor := httpconv.HTTPCommonAttrsExtractor[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse, httpconv.HTTPClientAttrsGetter[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse]]{
		HTTPGetter: clientGetter,
	}

	// Create network attributes extractor
	networkExtractor := netconv.CreateNetworkAttributesExtractor[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse](
		clientGetter,
	)

	// Create HTTP client attributes extractor
	httpClientExtractor := &httpconv.HTTPClientAttrsExtractor[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse, httpconv.HTTPClientAttrsGetter[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse]]{
		Base: commonExtractor,
	}

	// Create metrics registry and HTTP client metrics
	metricsRegistry := httpconv.NewMetricsRegistry(
		otelsetup.GetLogger(),
		otelsetup.GetMeterProvider().Meter(instrumentationName),
	)
	clientMetrics, err := metricsRegistry.NewHTTPClientMetric(instrumentationName)
	if err != nil {
		otelsetup.GetLogger().Error("failed to create HTTP client metrics", "error", err)
	}

	base := builder.Init().
		SetInstrumentEnabler(clientEnabler).
		SetSpanStatusExtractor(httpconv.HTTPClientSpanStatusExtractor[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse]{Getter: clientGetter}).
		SetSpanNameExtractor(&httpconv.HTTPClientSpanNameExtractor[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse]{Getter: clientGetter}).
		SetSpanKindExtractor(&instrumenter.AlwaysClientExtractor[*nethttp.NetHttpRequest]{}).
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
		func(req *nethttp.NetHttpRequest) propagation.TextMapCarrier {
			if req.Header() == nil {
				return nil
			}
			return propagation.HeaderCarrier(req.Header())
		},
		otel.GetTextMapPropagator(),
	)
}
