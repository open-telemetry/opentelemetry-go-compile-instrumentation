// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func H12UnderscoreParamReturnBefore(ctx hook.HookContext, p1 int) {}

func H12UnderscoreParamReturnAfter(ctx hook.HookContext, r1 error) {}
