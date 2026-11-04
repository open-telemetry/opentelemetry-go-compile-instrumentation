// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	_ "example.com/hookinit/driver"
	"example.com/hookinit/hooks"
	"example.com/hookinit/registry"
	"fmt"
)

func main() {
	registry.Register("late")
	b, a := hooks.Seen()
	fmt.Println("registered:", registry.Names)
	fmt.Println("before:", b)
	fmt.Println("after:", a)
}
