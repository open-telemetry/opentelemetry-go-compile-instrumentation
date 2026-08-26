// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"go.opentelemetry.io/otelc/pkg/runtime"
)

// instrumentationKey names this instrumentation for the runtime
// OTEL_GO_ENABLED_INSTRUMENTATIONS / OTEL_GO_DISABLED_INSTRUMENTATIONS lists.
const instrumentationKey = "FIBER"

var logger = runtime.Logger()

// fiberEnabler controls whether fiber instrumentation is enabled
type fiberEnabler struct{}

func (f fiberEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = fiberEnabler{}
