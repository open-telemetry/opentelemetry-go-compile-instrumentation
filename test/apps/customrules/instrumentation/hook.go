// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrumentation

import (
	"fmt"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func BeforeCustom(ctx hook.HookContext) {
	_ = ctx
	fmt.Println("custom hook called")
}
