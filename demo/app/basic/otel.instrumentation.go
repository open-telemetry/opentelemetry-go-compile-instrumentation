// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build tools

package tools

import (
	_ "go.opentelemetry.io/otelc/demo/app/basic/instrumentation"
	_ "go.opentelemetry.io/otelc/instrumentation/runtime" // required for GLS
)
