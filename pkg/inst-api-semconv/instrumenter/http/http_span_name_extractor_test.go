// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package http

import "testing"

type testRequest struct {
	Method string
	Route  string
}

type testResponse struct{}

type testClientGetter struct {
	HTTPClientAttrsGetter[testRequest, testResponse]
}

type testServerGetter struct {
	HTTPServerAttrsGetter[testRequest, testResponse]
}

func (testClientGetter) GetRequestMethod(request testRequest) string {
	if request.Method != "" {
		return request.Method
	}
	return ""
}

func (testServerGetter) GetRequestMethod(request testRequest) string {
	if request.Method != "" {
		return request.Method
	}
	return ""
}

func (testServerGetter) GetHTTPRoute(request testRequest) string {
	if request.Route != "" {
		return request.Route
	}
	return ""
}

func TestHTTPClientExtractSpanName(t *testing.T) {
	r := HTTPClientSpanNameExtractor[testRequest, testResponse]{Getter: testClientGetter{}}
	spanName := r.Extract(testRequest{Method: "GET"})
	if spanName != "GET" {
		t.Errorf("want GET, got %s", spanName)
	}
	spanName = r.Extract(testRequest{})
	if spanName != "HTTP" {
		t.Errorf("want HTTP, got %s", spanName)
	}
}

func TestHTTPServerExtractSpanName(t *testing.T) {
	r := HTTPServerSpanNameExtractor[testRequest, testResponse]{Getter: testServerGetter{}}
	spanName := r.Extract(testRequest{Method: "GET"})
	if spanName != "GET" {
		t.Errorf("want GET, got %s", spanName)
	}
	spanName = r.Extract(testRequest{})
	if spanName != "HTTP" {
		t.Errorf("want HTTP, got %s", spanName)
	}
	spanName = r.Extract(testRequest{Method: "GET", Route: "/a/b"})
	if spanName != "GET /a/b" {
		t.Errorf("want GET /a/b, got %s", spanName)
	}
}
