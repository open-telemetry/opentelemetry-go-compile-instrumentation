// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"go.opentelemetry.io/otelc/pkg/hook"
)

// The generic targets' accessors are generated for the instantiated type, so
// ctx.GetParam and ctx.GetReturnVal reach the values of a type parameter
// slot. The positional parameters below carry the same values.

func GenericFuncBefore(ctx hook.HookContext, p1 interface{}, p2 interface{}) {
	_ = ctx.GetParam(0)
	_ = ctx.GetParam(1)
}

func GenericFuncAfter(ctx hook.HookContext, r1 interface{}, r2 error) {
	_ = ctx.GetReturnVal(0)
}

func GenericMethodBefore(ctx hook.HookContext, recv interface{}, p1 interface{}, p2 string) {
	_ = ctx.GetParam(0)
	_ = ctx.GetParam(1)
}

func GenericMethodAfter(ctx hook.HookContext, r1 interface{}, r2 error) {
	_ = ctx.GetReturnVal(0)
}
