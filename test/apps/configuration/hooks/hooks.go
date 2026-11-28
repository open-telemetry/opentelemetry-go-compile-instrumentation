// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"fmt"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func BeforeMain(_ hook.HookContext) {
	fmt.Printf("hook-snapshot:%s\n", hook.ConfigurationSnapshot())
}
