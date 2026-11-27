// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
)

// A literal outside any function declaration.
var handler = func(ctx context.Context) {
	fmt.Println("package-level literal")
}

// The literal has no context, the declaration has one.
func Outer(ctx context.Context) {
	run := func() {
		fmt.Println("literal without ctx")
	}
	run()
	fmt.Println("declaration")
}

// The literal has its own context, the declaration has none.
func NoCtx() {
	run := func(c context.Context) {
		fmt.Println("literal with ctx")
	}
	run(context.Background())
}

func main() {
	handler(context.Background())
	Outer(context.Background())
	NoCtx()
}
