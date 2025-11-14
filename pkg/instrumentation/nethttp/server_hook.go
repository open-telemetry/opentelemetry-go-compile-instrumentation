// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nethttp

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	instrumenter "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
)

var (
	logger                 = shared.GetLogger()
	serverInstrumenter     *instrumenter.PropagatingFromUpstreamInstrumenter[*netHttpRequest, *netHttpResponse]
	serverInstrumenterOnce sync.Once
)

func getServerInstrumenter() *instrumenter.PropagatingFromUpstreamInstrumenter[*netHttpRequest, *netHttpResponse] {
	serverInstrumenterOnce.Do(func() {
		// Ensure SDK is initialized before building instrumenter
		if err := shared.SetupOTelSDK(); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}
		serverInstrumenter = BuildNetHttpServerOtelInstrumenter()
		logger.Info("HTTP server instrumentation initialized")
	})
	return serverInstrumenter
}

func BeforeServeHTTP(ictx inst.HookContext, recv interface{}, w http.ResponseWriter, r *http.Request) {
	if !serverEnabler.Enable() {
		logger.Debug("HTTP server instrumentation disabled")
		return
	}

	logger.Debug("BeforeServeHTTP called",
		"method", r.Method,
		"url", r.URL.String(),
		"remote_addr", r.RemoteAddr)

	// Build request representation
	request := &netHttpRequest{
		method:  r.Method,
		url:     r.URL,
		host:    r.Host,
		header:  r.Header,
		version: getProtocolVersion(r.ProtoMajor, r.ProtoMinor),
		isTls:   r.TLS != nil,
	}

	// Start instrumentation
	ctx := getServerInstrumenter().Start(r.Context(), request)

	// Wrap ResponseWriter to capture status code
	wrapper := &writerWrapper{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // Default status code
		wroteHeader:    false,
	}

	// Replace the ResponseWriter parameter with our wrapper
	ictx.SetParam(1, wrapper)

	// Store context and request for the after hook
	data := map[string]interface{}{
		"ctx":     ctx,
		"request": request,
		"start":   time.Now(),
	}
	ictx.SetData(data)

	logger.Debug("BeforeServeHTTP completed",
		"has_context", ctx != nil,
		"wrapped_writer", true)
}

func AfterServeHTTP(ictx inst.HookContext) {
	if !serverEnabler.Enable() {
		logger.Debug("HTTP server instrumentation disabled")
		return
	}

	// Retrieve data from before hook
	data, ok := ictx.GetData().(map[string]interface{})
	if !ok || data == nil {
		logger.Warn("AfterServeHTTP: no data from before hook")
		return
	}

	ctx, ok := data["ctx"].(context.Context)
	if !ok || ctx == nil {
		logger.Warn("AfterServeHTTP: no context from before hook")
		return
	}

	request, ok := data["request"].(*netHttpRequest)
	if !ok {
		logger.Warn("AfterServeHTTP: no request from before hook")
		return
	}

	startTime, ok := data["start"].(time.Time)
	if !ok {
		startTime = time.Now()
	}

	// Extract status code from wrapped ResponseWriter using GetParam
	// Parameter indices: 0=receiver, 1=ResponseWriter, 2=Request
	statusCode := http.StatusOK
	var responseHeader http.Header
	if p, ok := ictx.GetParam(1).(http.ResponseWriter); ok {
		if wrapper, ok := p.(*writerWrapper); ok {
			statusCode = wrapper.statusCode
		}
		responseHeader = p.Header()
	}

	// Get request from params for logging
	var r *http.Request
	if req, ok := ictx.GetParam(2).(*http.Request); ok {
		r = req
	}

	// Build response representation
	response := &netHttpResponse{
		statusCode: statusCode,
		header:     responseHeader,
	}

	duration := time.Since(startTime)
	if r != nil {
		logger.Debug("AfterServeHTTP called",
			"method", r.Method,
			"url", r.URL.String(),
			"status_code", statusCode,
			"duration_ms", duration.Milliseconds())
	} else {
		logger.Debug("AfterServeHTTP called",
			"status_code", statusCode,
			"duration_ms", duration.Milliseconds())
	}

	// End instrumentation
	getServerInstrumenter().End(ctx, instrumenter.Invocation[*netHttpRequest, *netHttpResponse]{
		Request:        request,
		Response:       response,
		Err:            nil,
		StartTimeStamp: startTime,
		EndTimeStamp:   time.Now(),
	})

	logger.Debug("AfterServeHTTP completed")
}
