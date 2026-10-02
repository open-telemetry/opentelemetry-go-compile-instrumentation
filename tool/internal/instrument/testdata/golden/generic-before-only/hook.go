// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"go.opentelemetry.io/otelc/pkg/hook"
)

// GetParam/SetParam/GetReturnVal/SetReturnVal panic for a parameter or return
// value whose type mentions the target's type parameter (see
// rewriteHookContextMethods), so this hook reads p1 (of type T) from its own
// positional parameter instead. This rule has no After hook at all, which
// exercises the optimizeTJumps path that removes the After trampoline but
// must leave those four HookContextImpl methods in place — see the comment
// on optimizeTJumps in optimize.go.
func GenericFuncBeforeOnlyBefore(ctx hook.HookContext, p1 interface{}, p2 int) {}
