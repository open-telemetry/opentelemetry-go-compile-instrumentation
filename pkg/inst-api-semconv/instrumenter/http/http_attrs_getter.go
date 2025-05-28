// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package http

type HttpCommonAttrsGetter[REQUEST any, RESPONSE any] interface {
	GetRequestMethod(request REQUEST) string
	GetHttpRequestHeader(request REQUEST, name string) []string
	GetHttpResponseStatusCode(request REQUEST, response RESPONSE, err error) int
	GetHttpResponseHeader(request REQUEST, response RESPONSE, name string) []string
	GetErrorType(request REQUEST, response RESPONSE, err error) string
}

type HttpServerAttrsGetter[REQUEST any, RESPONSE any] interface {
	HttpCommonAttrsGetter[REQUEST, RESPONSE]
	GetHttpRoute(request REQUEST) string
}

type HttpClientAttrsGetter[REQUEST any, RESPONSE any] interface {
	HttpCommonAttrsGetter[REQUEST, RESPONSE]
}
