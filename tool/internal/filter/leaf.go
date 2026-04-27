// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter

import "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"

var (
	_ Filter = (*FuncFilter)(nil)
	_ Filter = (*StructFilter)(nil)
)

// FuncFilter matches source files that declare the named function or method.
type FuncFilter struct {
	Func string
	Recv string
}

func (f *FuncFilter) Match(ctx *MatchContext) bool {
	return ast.FindFuncDecl(ctx.AST, f.Func, f.Recv) != nil
}

// StructFilter matches source files that declare the named struct.
type StructFilter struct {
	Struct string
}

func (f *StructFilter) Match(ctx *MatchContext) bool {
	return ast.FindStructDecl(ctx.AST, f.Struct) != nil
}
