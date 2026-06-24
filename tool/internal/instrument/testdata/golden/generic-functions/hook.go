// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
)

func GenericFuncBefore(ctx hook.HookContext, p1 interface{}, p2 int) {}

func GenericFuncAfter(ctx hook.HookContext, r1 interface{}, r2 error) {}

func GenericMethodBefore(ctx hook.HookContext, recv interface{}, p1 interface{}, p2 string) {}

func GenericMethodAfter(ctx hook.HookContext, r1 interface{}, r2 error) {}
