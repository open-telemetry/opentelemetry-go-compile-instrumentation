// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otel

import (
	"sync/atomic"

	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook"
)

var tracerProviderSet atomic.Bool

func beforeSetTracerProvider(ictx hook.HookContext, _ trace.TracerProvider) {
	if tracerProviderSet.Swap(true) {
		ictx.SetSkipCall(true)
	}
}
