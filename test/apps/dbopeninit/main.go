// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main uses a database that was opened during package initialization.
package main

import (
	"context"
	"log"

	"example.com/dbopeninit/early"
)

func main() {
	ctx := context.Background()
	defer early.DB.Close()
	if err := early.DB.PingContext(ctx); err != nil {
		log.Fatalf("ping failed: %v", err)
	}
	if _, err := early.DB.ExecContext(ctx, "INSERT INTO users (name) VALUES ('x')"); err != nil {
		log.Fatalf("insert failed: %v", err)
	}
}
