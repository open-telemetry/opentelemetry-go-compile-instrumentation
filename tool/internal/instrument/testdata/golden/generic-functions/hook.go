// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"go.opentelemetry.io/otelc/pkg/hook"
)

// These hooks read their values from their own parameters rather than through
// ctx.GetParam/GetReturnVal: those methods panic for generic targets, so the
// positional parameters below are the only way to reach the values.

func GenericFuncBefore(ctx hook.HookContext, p1 interface{}, p2 int) {}

func GenericFuncAfter(ctx hook.HookContext, r1 interface{}, r2 error) {}

func GenericMethodBefore(ctx hook.HookContext, recv interface{}, p1 interface{}, p2 string) {}

func GenericMethodAfter(ctx hook.HookContext, r1 interface{}, r2 error) {}
