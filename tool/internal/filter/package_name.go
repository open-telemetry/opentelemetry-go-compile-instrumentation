// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter

// Compile-time check that PackageNameFilter implements Filter.
var _ Filter = (*PackageNameFilter)(nil)

// PackageNameFilter matches source files whose declared package name equals
// the configured name. The declared package name is read from the AST
// (ctx.AST.Name.Name) — it is always present and is the same for every file
// in a Go package, so there is no need to store it separately on MatchContext.
type PackageNameFilter struct {
	Name string
}

// Match reports whether the package name declared in the source file equals
// f.Name.
func (f *PackageNameFilter) Match(ctx *MatchContext) bool {
	return ctx.AST.Name.Name == f.Name
}
