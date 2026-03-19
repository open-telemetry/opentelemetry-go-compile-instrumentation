// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter

// Compile-time check that PackageNameFilter implements Filter.
var _ Filter = (*PackageNameFilter)(nil)

// PackageNameFilter matches source files whose declared package name equals
// the configured name. The declared package name is read from the AST
// (ctx.AST.Name.Name). Non-test files in a package share the same name;
// external test files may declare a different name (e.g. "foo_test").
type PackageNameFilter struct {
	Name string
}

// Match reports whether the package name declared in the source file equals
// f.Name.
func (f *PackageNameFilter) Match(ctx *MatchContext) bool {
	return ctx.AST.Name.Name == f.Name
}
