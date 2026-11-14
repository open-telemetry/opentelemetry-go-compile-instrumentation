// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main sends an HTTP request during package initialization and
// another one from main.
package main

import (
	"log"

	"example.com/httpinit/early"
)

func main() {
	if err := early.Get(); err != nil {
		log.Fatalf("request failed: %v", err)
	}
}
