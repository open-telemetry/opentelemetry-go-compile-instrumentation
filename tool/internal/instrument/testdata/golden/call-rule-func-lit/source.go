// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"go.opentelemetry.io/otelc/tool/internal/instrument/testdata/golden/call-rule-func-lit/helpers/logger"
)

var std = &logger.Logger{}

// No function holds the call.
var banner = logger.Log("package var")

// A function literal outside any function declaration.
var handler = func(ctx context.Context) {
	logger.Log("package literal")
	std.Info("package literal")
}

var plainHandler = func(name string) {
	logger.Log("package literal without ctx")
}

// A call directly in a declaration reads the declaration.
func direct(ctx context.Context) {
	logger.Log("declaration")
	std.Info("declaration")
}

// The literal has its own ctx, the declaration has none.
func literalCtx() {
	f := func(ctx context.Context) {
		logger.Log("literal")
		std.Info("literal")
	}
	f(context.Background())
}

// The innermost function wins over the declaration.
func innermost(ctx context.Context) {
	f := func(inner context.Context) {
		logger.Log("inner literal")
	}
	f(ctx)
	// The literal's ctx is a string, which shadows the declaration's ctx.
	g := func(ctx string) {
		logger.Log("shadowed ctx")
	}
	g("x")
	// A literal without a ctx does not read the declaration's ctx.
	h := func() {
		std.Info("literal without ctx")
	}
	h()
}

func nested(ctx context.Context) {
	func(outer context.Context) {
		func(inner context.Context) {
			logger.Log("nested")
		}(outer)
		logger.Log("after nested")
	}(ctx)
}

func goAndDefer(ctx context.Context) {
	func(inner context.Context) {
		defer logger.Log("deferred")
		go std.Info("go")
	}(ctx)
}

type server struct{}

// FuncName is "" in a function literal.
func (s *server) handle() {
	logger.Name("method")
	func() {
		logger.Name("literal in method")
	}()
}

func main() {
	_ = banner
	handler(context.Background())
	plainHandler("x")
	direct(context.Background())
	literalCtx()
	innermost(context.Background())
	nested(context.Background())
	goAndDefer(context.Background())
	(&server{}).handle()
}
