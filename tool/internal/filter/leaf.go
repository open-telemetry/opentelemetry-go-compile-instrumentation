// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
)

// Compile-time checks that FuncFilter and StructFilter implement Filter.
// Placing these in the production package (not test) catches interface drift
// for any build that includes this package, not only test builds.
var (
	_ Filter = (*FuncFilter)(nil)
	_ Filter = (*StructFilter)(nil)
)

// FuncFilter matches source files that contain a function declaration with the
// given name and optional receiver type.
//
// When Recv is empty, only free functions (no receiver) are matched.
// When Recv is non-empty, the receiver type must also match.
type FuncFilter struct {
	Func string
	Recv string // optional; empty means free function only
}

// Match reports whether the source file in ctx contains the target function.
func (f *FuncFilter) Match(ctx *MatchContext) bool {
	return ast.FindFuncDecl(ctx.AST, f.Func, f.Recv) != nil
}

// StructFilter matches source files that contain a struct type declaration with
// the given name.
type StructFilter struct {
	Struct string
}

// Match reports whether the source file in ctx contains the target struct.
func (f *StructFilter) Match(ctx *MatchContext) bool {
	return ast.FindStructDecl(ctx.AST, f.Struct) != nil
}
