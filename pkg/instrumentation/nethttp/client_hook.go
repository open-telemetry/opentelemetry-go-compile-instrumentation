// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nethttp

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	instrumenter "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
)

const otelExporterPrefix = "OTel OTLP Exporter Go"

var clientInstrumenter = BuildNetHttpClientOtelInstrumenter()

func init() {
	// Setup OpenTelemetry SDK (idempotent, shared across all instrumentations)
	if err := shared.SetupOTelSDK(); err != nil {
		logger.Error("failed to setup OTel SDK", "error", err)
		return
	}
	logger.Info("HTTP client instrumentation initialized")
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
	request := &netHttpRequest{
		method:  req.Method,
		url:     req.URL,
		host:    req.Host,
		header:  req.Header,
		version: getProtocolVersion(req.ProtoMajor, req.ProtoMinor),
		isTls:   req.TLS != nil,
	}

	// Start instrumentation (this will inject trace context into headers)
	ctx := clientInstrumenter.Start(req.Context(), request)

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

	request, ok := data["request"].(*netHttpRequest)
	if !ok {
		logger.Debug("AfterRoundTrip: no request from before hook")
		return
	}

	startTime, ok := data["start"].(time.Time)
	if !ok {
		startTime = time.Now()
	}

	// Build response representation
	var response *netHttpResponse
	if res != nil {
		response = &netHttpResponse{
			statusCode: res.StatusCode,
			header:     res.Header,
		}

		// Update request with actual values from response
		request.method = res.Request.Method
		request.url = res.Request.URL
		request.header = res.Request.Header
		request.version = getProtocolVersion(res.Request.ProtoMajor, res.Request.ProtoMinor)
		request.host = res.Request.Host
		request.isTls = res.Request.TLS != nil

		duration := time.Since(startTime)
		logger.Debug("AfterRoundTrip called",
			"method", res.Request.Method,
			"url", res.Request.URL.String(),
			"status_code", res.StatusCode,
			"duration_ms", duration.Milliseconds())
	} else {
		// Error case: no response
		response = &netHttpResponse{
			statusCode: 500,
		}
		logger.Debug("AfterRoundTrip called with error",
			"error", err,
			"duration_ms", time.Since(startTime).Milliseconds())
	}

	// End instrumentation
	clientInstrumenter.End(ctx, instrumenter.Invocation[*netHttpRequest, *netHttpResponse]{
		Request:        request,
		Response:       response,
		Err:            err,
		StartTimeStamp: startTime,
		EndTimeStamp:   time.Now(),
	})

	logger.Debug("AfterRoundTrip completed")
}
