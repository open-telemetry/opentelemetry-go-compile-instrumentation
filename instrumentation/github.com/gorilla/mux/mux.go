// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package server enriches the net/http server span with gorilla/mux's matched route.
// See schemas/otelc/groups/mux.yaml for the emission contract.
package server

import (
	"go.opentelemetry.io/otelc/pkg/runtime"
)

// instrumentationKey names this instrumentation for
// OTEL_GO_ENABLED_INSTRUMENTATIONS / OTEL_GO_DISABLED_INSTRUMENTATIONS.
// Mux only enriches the span created by the net/http server instrumentation.
const instrumentationKey = "MUX"

var logger = runtime.Logger()

type muxEnabler struct{}

func (muxEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = muxEnabler{}
