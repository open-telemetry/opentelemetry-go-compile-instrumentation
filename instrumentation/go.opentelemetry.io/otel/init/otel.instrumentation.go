// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// The init module links pkg/runtime, which links the OTel trace API and the
// SDK trace package. otelc reads this nested tool file, so their rules load
// with this module even when the application does not import them (#1712).

//go:build tools

package tools

import (
	_ "go.opentelemetry.io/otelc/instrumentation/go.opentelemetry.io/otel/sdk/trace"
	_ "go.opentelemetry.io/otelc/instrumentation/go.opentelemetry.io/otel/trace"
)
