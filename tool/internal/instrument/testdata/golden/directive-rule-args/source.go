// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

//otelc:span
func divide(a int, b int) (int, error) {
	return a / b, nil
}

func main() { divide(4, 2) }
