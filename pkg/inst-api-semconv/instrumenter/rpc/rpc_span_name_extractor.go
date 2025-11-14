// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rpc

// RpcSpanNameExtractor generates span names for RPC operations
// following OpenTelemetry semantic conventions.
type RpcSpanNameExtractor[REQUEST any] struct {
	Getter RpcAttrsGetter[REQUEST]
}

// Extract generates a span name from an RPC request.
// The format is "{service}/{method}" (e.g., "/helloworld.Greeter/SayHello").
// If either service or method is empty, returns "RPC request".
func (r *RpcSpanNameExtractor[REQUEST]) Extract(request REQUEST) string {
	service := r.Getter.GetService(request)
	method := r.Getter.GetMethod(request)

	if service == "" || method == "" {
		return "RPC request"
	}

	return service + "/" + method
}
