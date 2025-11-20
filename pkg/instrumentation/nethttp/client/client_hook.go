// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	instrumenter "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
)

const otelExporterPrefix = "OTel OTLP Exporter Go"

var (
	logger                 = shared.GetLogger()
	clientInstrumenter     *instrumenter.PropagatingToDownstreamInstrumenter[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse]
	clientInstrumenterOnce sync.Once
)

func getClientInstrumenter() *instrumenter.PropagatingToDownstreamInstrumenter[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse] {
	clientInstrumenterOnce.Do(func() {
		// Ensure SDK is initialized before building instrumenter
		if err := shared.SetupOTelSDK(); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}
		clientInstrumenter = BuildNetHttpClientOtelInstrumenter()
		logger.Info("HTTP client instrumentation initialized")
	})
	return clientInstrumenter
}

func BeforeRoundTrip(ictx inst.HookContext, transport *http.Transport, req *http.Request) {
	if !clientEnabler.Enable() {
		logger.Debug("HTTP client instrumentation disabled")
		return
	}

	// Filter out requests from OpenTelemetry HTTP Exporter to prevent infinite loops
	userAgent := req.Header.Get("User-Agent")
	if strings.HasPrefix(userAgent, otelExporterPrefix) {
		logger.Debug("Skipping OTel exporter request", "user_agent", userAgent)
		return
	}

	logger.Debug("BeforeRoundTrip called",
		"method", req.Method,
		"url", req.URL.String(),
		"host", req.Host)

	// Build request representation
	request := nethttp.NewNetHttpRequest(
		req.Method,
		req.URL,
		req.Host,
		req.Header,
		nethttp.GetProtocolVersion(req.ProtoMajor, req.ProtoMinor),
		req.TLS != nil,
	)

	// Start instrumentation (this will inject trace context into headers)
	ctx := getClientInstrumenter().Start(req.Context(), request)

	// Update request with new context (contains trace information)
	newReq := req.WithContext(ctx)

	// Replace the request parameter with updated request
	ictx.SetParam(1, newReq)

	// Store context and request for the after hook
	data := map[string]interface{}{
		"ctx":     ctx,
		"request": request,
		"start":   time.Now(),
	}
	ictx.SetData(data)

	logger.Debug("BeforeRoundTrip completed",
		"has_context", ctx != nil,
		"trace_injected", true)
}

func AfterRoundTrip(ictx inst.HookContext, res *http.Response, err error) {
	if !clientEnabler.Enable() {
		logger.Debug("HTTP client instrumentation disabled")
		return
	}

	// Retrieve data from before hook
	data, ok := ictx.GetData().(map[string]interface{})
	if !ok || data == nil {
		logger.Debug("AfterRoundTrip: no data from before hook")
		return
	}

	ctx, ok := data["ctx"].(context.Context)
	if !ok || ctx == nil {
		logger.Debug("AfterRoundTrip: no context from before hook")
		return
	}

	request, ok := data["request"].(*nethttp.NetHttpRequest)
	if !ok {
		logger.Debug("AfterRoundTrip: no request from before hook")
		return
	}

	startTime, ok := data["start"].(time.Time)
	if !ok {
		startTime = time.Now()
	}

	// Build response representation
	var response *nethttp.NetHttpResponse
	if res != nil {
		response = nethttp.NewNetHttpResponse(res.StatusCode, res.Header)

		// Update request with actual values from response
		request = nethttp.NewNetHttpRequest(
			res.Request.Method,
			res.Request.URL,
			res.Request.Host,
			res.Request.Header,
			nethttp.GetProtocolVersion(res.Request.ProtoMajor, res.Request.ProtoMinor),
			res.Request.TLS != nil,
		)

		duration := time.Since(startTime)
		logger.Debug("AfterRoundTrip called",
			"method", res.Request.Method,
			"url", res.Request.URL.String(),
			"status_code", res.StatusCode,
			"duration_ms", duration.Milliseconds())
	} else {
		// Error case: no response
		response = nethttp.NewNetHttpResponse(500, nil)
		logger.Debug("AfterRoundTrip called with error",
			"error", err,
			"duration_ms", time.Since(startTime).Milliseconds())
	}

	// End instrumentation
	getClientInstrumenter().End(ctx, instrumenter.Invocation[*nethttp.NetHttpRequest, *nethttp.NetHttpResponse]{
		Request:        request,
		Response:       response,
		Err:            err,
		StartTimeStamp: startTime,
		EndTimeStamp:   time.Now(),
	})

	logger.Debug("AfterRoundTrip completed")
}
