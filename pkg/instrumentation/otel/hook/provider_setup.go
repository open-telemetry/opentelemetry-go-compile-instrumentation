// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hook

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"go.opentelemetry.io/otel/trace"
)

//go:linkname setGlobalProviderEnable go.opentelemetry.io/otel.SetGlobalProviderEnable
var setGlobalProviderEnable bool

func setTracerProviderOnEnter(ictx inst.HookContext, tp trace.TracerProvider) {
	if setGlobalProviderEnable {
		ictx.SetSkipCall(true)
		return
	}
	setGlobalProviderEnable = true
}
