// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// A minimal gRPC client application used to verify that instrumentation
// remains compatible with older gRPC versions during build/setup.
package main

import (
	"context"

	"google.golang.org/grpc"
)

func main() {
	_, _ = grpc.DialContext(context.TODO(), "")
}
