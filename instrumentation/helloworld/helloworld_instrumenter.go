// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package helloworld

import (
	"log/slog"

	instrumenter "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/http"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/net"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/instrumentation"
)

type HelloWorldRequest struct{}

type HelloWorldResponse struct{}

type HelloWorldAttributesGetter struct{}

type helloWorldSpanNameExtractor struct{}

func (h helloWorldSpanNameExtractor) Extract(request HelloWorldRequest) string {
	return "hello-world"
}

func (h HelloWorldAttributesGetter) GetURLScheme(request HelloWorldRequest) string {
	return "http"
}

func (h HelloWorldAttributesGetter) GetURLPath(request HelloWorldRequest) string {
	return "/a"
}

func (h HelloWorldAttributesGetter) GetURLQuery(request HelloWorldRequest) string {
	return "a=5"
}

func BuildNetHttpClientOtelInstrumenter() instrumenter.Instrumenter[HelloWorldRequest, HelloWorldResponse] {
	builder := &instrumenter.Builder[HelloWorldRequest, HelloWorldResponse]{}
	helloWorldGetter := HelloWorldAttributesGetter{}
	urlAttributesExtractor := &net.URLAttrsExtractor[HelloWorldRequest, HelloWorldResponse, HelloWorldAttributesGetter]{
		Getter: helloWorldGetter,
	}
	clientMetricRegistry := http.NewMetricsRegistry(slog.Default(), otel.GetMeterProvider().Meter("hello-world"))
	// TODO: return noop instrumenter when there is an error
	clientMetrics, _ := clientMetricRegistry.NewHTTPClientMetric("hello.world.client")
	return builder.Init().SetSpanNameExtractor(helloWorldSpanNameExtractor{}).
		SetSpanKindExtractor(&instrumenter.AlwaysInternalExtractor[HelloWorldRequest]{}).
		AddAttributesExtractor(urlAttributesExtractor).
		AddOperationListeners(clientMetrics).
		SetInstrumentationScope(instrumentation.Scope{
			Name:    "hello-world",
			Version: "0.0.1",
		}).BuildInstrumenter()
}
