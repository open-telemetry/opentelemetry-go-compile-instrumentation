// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
)

func Handler(r io.Reader, name string) {
	fmt.Println("hello", name)
}

func main() { Handler(nil, "world") }
