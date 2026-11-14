//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	_ "unsafe"
)

// The pkg/runtime functions are reached by linkname rather than by importing
// pkg/runtime: under go test this file goes into the testmain the go command
// generates, and no import can be added to that package. stub.go keeps
// pkg/runtime linked.
//
//go:linkname otelcSetupOTelSDK go.opentelemetry.io/otelc/pkg/runtime.SetupOTelSDK
func otelcSetupOTelSDK()

//go:linkname otelcStartRuntimeMetrics go.opentelemetry.io/otelc/pkg/runtime.StartRuntimeMetrics
func otelcStartRuntimeMetrics()

func init() {
	// Initialize OpenTelemetry SDK (sets up global tracer and meter providers)
	otelcSetupOTelSDK()

	// Start runtime metrics (respects OTEL_GO_ENABLED/DISABLED_INSTRUMENTATIONS)
	otelcStartRuntimeMetrics()
}
