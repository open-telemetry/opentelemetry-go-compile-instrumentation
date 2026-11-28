// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"go.opentelemetry.io/otelc/pkg/hook"
	_ "go.opentelemetry.io/otelc/test/apps/configuration/early"
)

func main() {
	fmt.Printf("main-snapshot:%s\n", hook.ConfigurationSnapshot())
}
