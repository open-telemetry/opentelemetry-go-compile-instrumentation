// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

func GenericFuncBefore(ctx inst.HookContext, p1 interface{}, p2 int) {}

func GenericFuncAfter(ctx inst.HookContext, r1 interface{}, r2 error) {}

func GenericMethodBefore(ctx inst.HookContext, recv interface{}, p1 interface{}, p2 string) {}

func GenericMethodAfter(ctx inst.HookContext, r1 interface{}, r2 error) {}
