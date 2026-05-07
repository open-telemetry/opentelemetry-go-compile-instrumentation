// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

func H1Before(ctx inst.HookContext, p1 string, p2 int) {
	println("H1Before")
}

func H1After(ctx inst.HookContext, r1 float32, r2 error) {}
