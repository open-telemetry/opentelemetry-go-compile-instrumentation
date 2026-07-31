// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"go.opentelemetry.io/otelc/pkg/runtime"
)

// instrumentationKey names this instrumentation for the runtime
// OTEL_GO_ENABLED_INSTRUMENTATIONS / OTEL_GO_DISABLED_INSTRUMENTATIONS lists.
// Gin only enriches the span created by the net/http server instrumentation,
// so it carries its own key: disabling gin drops the route enrichment while
// leaving HTTP server spans in place.
const instrumentationKey = "GIN"

var logger = runtime.Logger()

// ginEnabler controls whether gin instrumentation is enabled
type ginEnabler struct{}

func (g ginEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = ginEnabler{}
