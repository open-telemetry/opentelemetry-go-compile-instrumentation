// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "context"

type ReqCtx struct{ context.Context }

type MyErr struct{}

func (*MyErr) Error() string { return "" }

// HandleCustom matches: *ReqCtx implements context.Context and *MyErr
// implements error.
func HandleCustom(c *ReqCtx) *MyErr { return nil }

// HandlePlain does not match: no parameter implements context.Context.
func HandlePlain(n int) {}

func main() {
	HandleCustom(&ReqCtx{Context: context.Background()})
	HandlePlain(1)
}
