// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

func CustomTarget() {
	fmt.Println("custom target called")
}

func BaseTarget() {
	fmt.Println("base target called")
}

func main() {
	CustomTarget()
	BaseTarget()
}
