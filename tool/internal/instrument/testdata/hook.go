// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

//go:linkname H1Before main.H1Before
func H1Before(ctx inst.HookContext, p1 string, p2 int) {
	println("H1Before")
}

//go:linkname H1After main.H1After
func H1After(ctx inst.HookContext, r1 float32, r2 error) {}

//go:linkname H2Before main.H2Before
func H2Before(ctx inst.HookContext, p1 string, p2 int) {}

//go:linkname H2After main.H2After
func H2After(ctx inst.HookContext, r1 float32, r2 error) {}

//go:linkname H3Before main.H3Before
func H3Before(ctx inst.HookContext, recv interface{}, p1 string, p2 int) {}
