// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import "go.opentelemetry.io/otelc/pkg/hook"

func Before0(ctx hook.HookContext)                             {}
func After0(ctx hook.HookContext, result int)                  {}
func Before3(ctx hook.HookContext, p0, p1, p2 int)             {}
func After3(ctx hook.HookContext, result int)                  {}
func Before6(ctx hook.HookContext, p0, p1, p2, p3, p4, p5 int) {}
func After6(ctx hook.HookContext, result int)                  {}
