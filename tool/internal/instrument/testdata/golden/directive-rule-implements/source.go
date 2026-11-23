// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "context"

// RequestContext implements context.Context through its embedded field.
type RequestContext struct{ context.Context }

type MyErr struct{}

func (*MyErr) Error() string { return "" }

//otelc:span
func plain(ctx context.Context) error { return nil }

//otelc:span
func framework(n int, c *RequestContext) (int, *MyErr) { return n, nil }

//otelc:span
func none(n int) MyErr { return MyErr{} }

func main() {
	_ = plain(context.Background())
	framework(1, &RequestContext{Context: context.Background()})
	none(1)
}
