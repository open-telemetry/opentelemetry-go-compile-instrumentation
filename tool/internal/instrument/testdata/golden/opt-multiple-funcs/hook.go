// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
)

func H5Before(ctx inst.HookContext) {}

func H6Before(ctx inst.HookContext) { _ = ctx }

func H7Before(ctx inst.HookContext) { ctx.SetSkipCall(true) }

func H7After(ctx inst.HookContext) { _ = ctx }
