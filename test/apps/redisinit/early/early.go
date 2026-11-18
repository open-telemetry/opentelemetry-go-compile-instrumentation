// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package early creates a Redis client in a package-level variable and uses it
// during package initialization. Its import path sorts before
// go.opentelemetry.io, so it initializes before the OpenTelemetry packages.
package early

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// Client points at a closed port, so every command fails to dial.
var Client = redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1})

func init() {
	_ = Client.Ping(context.Background()).Err()
}
