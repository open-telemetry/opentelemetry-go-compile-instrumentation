// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"go.opentelemetry.io/otelc/pkg/hook"
)

// This before-only generic hook exercises optimizeTJumps while retaining
// the HookContext methods, including accessors that panic for T.
func GenericFuncBeforeOnlyBefore(ctx hook.HookContext, p1 interface{}, p2 int) {}
