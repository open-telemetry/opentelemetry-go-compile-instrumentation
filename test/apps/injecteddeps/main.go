// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Command injecteddeps is an application that imports nothing from the module
// carrying its instrumentation rules, which is the ordinary case for a real
// application. It only uses net/http.
package main

import (
	"fmt"
	"net/http"
)

func main() {
	// Hooked by the instrumentation module. The hook reports whether the rule
	// targeting that module's own target package applied.
	mux := http.NewServeMux()
	_ = mux

	fmt.Println("injecteddeps: done")
}
