// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package early

import (
	"fmt"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func init() {
	fmt.Printf("dependency-snapshot:%s\n", hook.ConfigurationSnapshot())
}
