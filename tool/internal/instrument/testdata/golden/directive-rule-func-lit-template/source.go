// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "context"

type failedError struct{}

func (failedError) Error() string { return "failed" }

var errFailed error = failedError{}

func main() {
	//otelc:span span.name:"unnamed-results"
	divide := func(ctx context.Context, a int, b int) (int, error) {
		if b == 0 {
			return 0, errFailed
		}
		return a / b, nil
	}
	_, _ = divide(context.Background(), 4, 0)

	//otelc:span span.name:"named-results"
	named := func(n int) (out int, err error) {
		return n, nil
	}
	_, _ = named(1)

	//otelc:span span.name:"unnamed-params"
	unnamed := func(context.Context, int) error {
		return nil
	}
	_ = unnamed(context.Background(), 1)

	//otelc:span
	noArgs := func() {}
	noArgs()
}
