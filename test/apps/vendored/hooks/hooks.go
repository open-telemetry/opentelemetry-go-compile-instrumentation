// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"fmt"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func Before(ctx hook.HookContext) {
	_ = ctx
	fmt.Println("vendored hook executed")
}
