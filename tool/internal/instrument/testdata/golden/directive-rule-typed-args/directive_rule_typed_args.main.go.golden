// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "context"

//otelc:span span.name:"custom-op"
func divide(ctx context.Context, a int, b int) (int, error) {
	return a / b, nil
}

func main() { divide(context.Background(), 4, 2) }
