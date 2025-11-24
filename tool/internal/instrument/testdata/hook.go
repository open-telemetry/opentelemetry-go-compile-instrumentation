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

func H2Before(ctx inst.HookContext, p1 string, p2 int) {}

func H2After(ctx inst.HookContext, r1 float32, r2 error) {}

func H3Before(ctx inst.HookContext, recv interface{}, p1 string, p2 int) {}

func H3After(ctx inst.HookContext, r1 float32, r2 error) {}

func H4Before(ctx inst.HookContext, p1 string, _ int) {}

func H5Before(ctx inst.HookContext) {}

func H6Before(ctx inst.HookContext) { _ = ctx }

func H7Before(ctx inst.HookContext) { ctx.SetSkipCall(true) }

func H7After(ctx inst.HookContext) { _ = ctx }

func H8After(ctx inst.HookContext, ret1 float32, ret2 error) {}

func GenericFuncBefore(ctx inst.HookContext, p1 interface{}, p2 int) {}

func GenericFuncAfter(ctx inst.HookContext, r1 interface{}, r2 error) {}

func GenericMethodBefore(ctx inst.HookContext, recv interface{}, p1 interface{}, p2 string) {}

func GenericMethodAfter(ctx inst.HookContext, r1 interface{}, r2 error) {}
