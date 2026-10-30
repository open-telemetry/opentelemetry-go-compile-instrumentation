// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

// The directive annotates a statement that holds no function literal, so the
// rule matches no function and must not inject anything -- including its imports.
func main() {
	//otelc:span
	value := 1
	println(value)
}
