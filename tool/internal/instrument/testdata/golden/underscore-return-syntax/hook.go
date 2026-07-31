// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testdata

import (
	_ "unsafe"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func H12UnderscoreReturnAfter(ctx hook.HookContext, r1 int, r2 error) {}
