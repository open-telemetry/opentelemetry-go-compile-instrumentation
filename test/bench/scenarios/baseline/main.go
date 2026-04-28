// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main is the baseline benchmark scenario.
//
// It imports only the Go standard library so that otelc has no instrumentation
// rules to apply.
package main

import "fmt"

func main() {
	fmt.Println("baseline scenario")
}
