// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

type Handler string

func (h Handler) Serve(name string) {
	fmt.Println("hello")
}

func main() { Handler("svc").Serve("world") }
