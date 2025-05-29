// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package http

/**
HTTP span names SHOULD be {method} {target} if there is a (low-cardinality) target available.
If there is no (low-cardinality) {target} available, HTTP span names SHOULD be {method}.
*/

const defaultHTTPSpanName = "HTTP"

type HTTPClientSpanNameExtractor[REQUEST any, RESPONSE any] struct {
	Getter HTTPClientAttrsGetter[REQUEST, RESPONSE]
}

func (h *HTTPClientSpanNameExtractor[REQUEST, RESPONSE]) Extract(request REQUEST) string {
	method := h.Getter.GetRequestMethod(request)
	if method == "" {
		return defaultHTTPSpanName
	}
	return method
}

type HTTPServerSpanNameExtractor[REQUEST any, RESPONSE any] struct {
	Getter HTTPServerAttrsGetter[REQUEST, RESPONSE]
}

func (h *HTTPServerSpanNameExtractor[REQUEST, RESPONSE]) Extract(request REQUEST) string {
	method := h.Getter.GetRequestMethod(request)
	route := h.Getter.GetHTTPRoute(request)
	if method == "" {
		return defaultHTTPSpanName
	}
	if route == "" {
		return method
	}
	return method + " " + route
}
