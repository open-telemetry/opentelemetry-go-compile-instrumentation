// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func H1Before(ctx hook.HookContext, c interface{}) {}

func H1After(ctx hook.HookContext, err interface{}) {}

func H2Before(ctx hook.HookContext, n int) {}
