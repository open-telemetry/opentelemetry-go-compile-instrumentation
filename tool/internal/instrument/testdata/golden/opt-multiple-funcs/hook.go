// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/hook"
)

func H5Before(ctx hook.HookContext) {}

func H6Before(ctx hook.HookContext) { _ = ctx }

func H7Before(ctx hook.HookContext) { ctx.SetSkipCall(true) }

func H7After(ctx hook.HookContext) { _ = ctx }
