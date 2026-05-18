// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
)

const instrumentationKey = "GIN"

var logger = shared.Logger()

// ginEnabler controls whether gin instrumentation is enabled.
type ginEnabler struct{}

func (g ginEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var serverEnabler = ginEnabler{}
