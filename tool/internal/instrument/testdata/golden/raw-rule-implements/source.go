// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "context"

type RequestContext struct{ context.Context }

type MyErr struct{}

func (*MyErr) Error() string { return "" }

func handle(_ string, c *RequestContext) (int, *MyErr) {
	return 0, nil
}

func main() { handle("x", &RequestContext{Context: context.Background()}) }
