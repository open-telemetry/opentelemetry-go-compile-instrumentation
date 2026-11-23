// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"golang.org/x/time/rate"
)

func RunApp() {
	limiter := rate.NewLimiter(1, 1)
	fmt.Println("rate limit:", limiter.Limit())
}

func main() {
	RunApp()
}
