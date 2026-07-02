//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/runtime"
)

func init() {
	// Initialize OpenTelemetry SDK (sets up global tracer and meter providers)
	runtime.SetupOTelSDK()

	// Start runtime metrics (respects OTEL_GO_ENABLED/DISABLED_INSTRUMENTATIONS)
	runtime.StartRuntimeMetrics()
}
