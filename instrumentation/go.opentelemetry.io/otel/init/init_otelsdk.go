//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go.opentelemetry.io/otelc/pkg/runtime"
)

func init() {
	// Initialize OpenTelemetry SDK (sets up global tracer and meter providers)
	runtime.SetupOTelSDK()

	// Start runtime metrics (respects OTEL_GO_ENABLED/DISABLED_INSTRUMENTATIONS)
	runtime.StartRuntimeMetrics()
}
