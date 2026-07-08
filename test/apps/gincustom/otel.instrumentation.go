// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build tools

package tools

import (
	_ "go.opentelemetry.io/otelc/instrumentation/go.opentelemetry.io/otel/init"
	_ "go.opentelemetry.io/otelc/test/apps/gincustom/instrumentation"
)
