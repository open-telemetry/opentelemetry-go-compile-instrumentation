// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package server enriches the net/http server span with Echo's matched route.
// See schemas/otelc/groups/echo.yaml for the emission contract.
package server

import (
	"go.opentelemetry.io/otelc/pkg/runtime"
)

// instrumentationKey names this instrumentation for
// OTEL_GO_ENABLED_INSTRUMENTATIONS / OTEL_GO_DISABLED_INSTRUMENTATIONS.
// Echo only enriches the span created by the net/http server instrumentation.
const instrumentationKey = "ECHO"

var logger = runtime.Logger()

type echoEnabler struct{}

func (echoEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = echoEnabler{}
