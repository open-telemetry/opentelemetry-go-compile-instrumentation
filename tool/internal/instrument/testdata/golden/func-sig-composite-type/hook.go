// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func H1Before(ctx hook.HookContext, p1 []byte, p2 map[string]any) {
	println("H1Before")
}

func H1After(ctx hook.HookContext, r1 chan struct{}, r2 error) {}
