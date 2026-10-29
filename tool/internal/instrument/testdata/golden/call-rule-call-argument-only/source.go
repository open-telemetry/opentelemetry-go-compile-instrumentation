// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"
)

func Greet(name string) string {
	return strings.ToUpper(name)
}

func main() {
	fmt.Println("hello", Greet("world"))
}
