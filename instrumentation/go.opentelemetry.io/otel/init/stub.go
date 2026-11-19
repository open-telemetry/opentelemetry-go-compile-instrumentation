// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// This file keeps the package importable. The implementation lives in files
// marked with `//go:build ignore`, which are consumed by otelc during
// instrumentation.
package init

// Import these packages to keep them in go.mod. otel.instrumentation.go loads
// the rules of the two instrumentation modules (#1712).
import (
	// Keeps the SDK trace instrumentation module in go.mod.
	_ "go.opentelemetry.io/otelc/instrumentation/go.opentelemetry.io/otel/sdk/trace"
	// Keeps the trace API instrumentation module in go.mod.
	_ "go.opentelemetry.io/otelc/instrumentation/go.opentelemetry.io/otel/trace"
	// Keeps the runtime package in go.mod.
	_ "go.opentelemetry.io/otelc/pkg/runtime"
)
