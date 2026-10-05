// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

func Handler(name string) {
	fmt.Println("hello")
}

func main() { Handler("world") }
