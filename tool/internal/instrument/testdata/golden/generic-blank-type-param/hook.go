// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func LenBefore(ctx hook.HookContext, recv interface{}) {}

func LenAfter(ctx hook.HookContext, n int) {}

func KeyBefore(ctx hook.HookContext, recv interface{}) {}

func KeyAfter(ctx hook.HookContext, key interface{}) {}
