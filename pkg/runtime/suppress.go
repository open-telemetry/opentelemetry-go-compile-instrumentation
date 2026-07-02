// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import "context"

type contextKey struct{}

var suppressHTTPClientKey = contextKey{}

// SuppressHTTPClientInstrumentation returns a context that signals the net/http
// client hook to skip span creation. Use this from higher-level instrumentations
// (e.g., GenAI) that already create a more specific span.
func SuppressHTTPClientInstrumentation(ctx context.Context) context.Context {
	return context.WithValue(ctx, suppressHTTPClientKey, true)
}

// IsHTTPClientInstrumentationSuppressed reports whether the context carries the
// suppression flag set by SuppressHTTPClientInstrumentation.
func IsHTTPClientInstrumentationSuppressed(ctx context.Context) bool {
	v, _ := ctx.Value(suppressHTTPClientKey).(bool)
	return v
}
