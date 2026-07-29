// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"go.opentelemetry.io/otelc/pkg/runtime"
)

var logger = runtime.Logger()

const instrumentationKey = "GIN"

// ginEnabler controls whether gin instrumentation is enabled.
type ginEnabler struct{}

func (ginEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = ginEnabler{}
