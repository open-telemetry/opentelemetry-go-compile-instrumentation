// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package http

type HTTPCommonAttrsGetter[REQUEST any, RESPONSE any] interface {
	GetRequestMethod(request REQUEST) string
	GetHTTPRequestHeader(request REQUEST, name string) []string
	GetHTTPResponseStatusCode(request REQUEST, response RESPONSE, err error) int
	GetHTTPResponseHeader(request REQUEST, response RESPONSE, name string) []string
	GetErrorType(request REQUEST, response RESPONSE, err error) string
}

type HTTPServerAttrsGetter[REQUEST any, RESPONSE any] interface {
	HTTPCommonAttrsGetter[REQUEST, RESPONSE]
	GetHTTPRoute(request REQUEST) string
}

type HTTPClientAttrsGetter[REQUEST any, RESPONSE any] interface {
	HTTPCommonAttrsGetter[REQUEST, RESPONSE]
}
