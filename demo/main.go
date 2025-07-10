// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fmt"
	_ "unsafe"

	_ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/helloworld"
)

func main() {
	fmt.Println("Hello World")
}
