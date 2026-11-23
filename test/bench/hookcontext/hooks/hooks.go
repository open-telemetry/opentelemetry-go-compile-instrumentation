// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import "go.opentelemetry.io/otelc/pkg/hook"

func Before0(ctx hook.HookContext)                                                 {}
func After0(ctx hook.HookContext, result int)                                      {}
func Before3(ctx hook.HookContext, p0 int, p1 int, p2 int)                         {}
func After3(ctx hook.HookContext, result int)                                      {}
func Before6(ctx hook.HookContext, p0 int, p1 int, p2 int, p3 int, p4 int, p5 int) {}
func After6(ctx hook.HookContext, result int)                                      {}
