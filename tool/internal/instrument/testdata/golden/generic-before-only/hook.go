// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"go.opentelemetry.io/otelc/pkg/hook"
)

// The generic target's accessors are generated for the instantiated type, so
// ctx.GetParam and friends reach the values of a type parameter slot. This
// rule has no After hook at all, which exercises the optimizeTJumps path that
// removes the After trampoline but must leave those four HookContextImpl
// methods in place — see the comment on optimizeTJumps in optimize.go.
func GenericFuncBeforeOnlyBefore(ctx hook.HookContext, p1 interface{}, p2 int) {}
