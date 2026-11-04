// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import "go.opentelemetry.io/otelc/pkg/hook"

func UnusedBefore(ctx hook.HookContext, n int) { println(n) }
func UnusedAfter(_ hook.HookContext, n int) { println(n) }
func UsedBefore(ctx hook.HookContext, n int) { ctx.SetData(n) }
func UsedAfter(ctx hook.HookContext, n int) { ctx.SetReturnVal(0, n+1) }
