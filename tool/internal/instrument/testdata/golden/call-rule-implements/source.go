// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"go.opentelemetry.io/otelc/tool/internal/instrument/testdata/golden/call-rule-implements/helpers/framework"
)

func handle(name string, c *framework.Context) {
	fmt.Println("hello", name)
}

func main() { handle("world", &framework.Context{}) }
