// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package base

import (
	"fmt"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func BeforeBase(ctx hook.HookContext) {
	_ = ctx
	fmt.Println("base hook called")
}
