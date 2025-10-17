// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

//go:linkname Func1Before main.Func1Before
func Func1Before(ctx inst.HookContext) {
	println("Func1Before")
}

//go:linkname Func1After main.Func1After
func Func1After(ctx inst.HookContext) {
	println("Func1After")
}
