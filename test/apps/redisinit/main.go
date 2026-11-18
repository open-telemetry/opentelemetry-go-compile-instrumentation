// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main uses a Redis client that another package used during
// initialization.
package main

import (
	"context"

	"example.com/redisinit/early"
)

func main() {
	defer early.Client.Close()
	_ = early.Client.Ping(context.Background()).Err()
}
