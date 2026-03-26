// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hook

import (
	"sync"
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"go.opentelemetry.io/otel/trace"
)

//go:linkname setGlobalProviderEnable go.opentelemetry.io/otel.setGlobalProviderEnable
var setGlobalProviderEnable bool
var setGlobalProviderOnce sync.Once

func setTracerProviderOnEnter(ictx inst.HookContext, tp trace.TracerProvider) {
	firstCall := false
	setGlobalProviderOnce.Do(func() {
		setGlobalProviderEnable = true
		firstCall = true
	})
	if !firstCall {
		ictx.SetSkipCall(true)
		return
	}
}
