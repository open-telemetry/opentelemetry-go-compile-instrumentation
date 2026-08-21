// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"path/filepath"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/util"
)

// packageGoFiles returns the absolute paths of every .go source file in the
// current package compile, derived from ip.compileArgs. A single toolexec
// invocation corresponds to one `go tool compile` call for one package, which
// receives every source file of that package as a positional argument, so
// this is the current package's complete file set -- no setup-phase plumbing
// needed.
//
// CGO-processed files are not resolved here: unlike the setup phase's
// findGoSources, this does not replicate CGO object-dir resolution. A .go arg
// pointing at a path that doesn't exist on disk (as can happen for
// CGO-generated intermediates) is silently skipped rather than resolved.
func (ip *instrumentPhase) packageGoFiles() []string {
	files := make([]string, 0, len(ip.compileArgs))
	for _, arg := range ip.compileArgs {
		if !util.IsGoFile(arg) {
			continue
		}
		abs, err := filepath.Abs(arg)
		if err != nil {
			ip.Warn(
				"failed to resolve absolute path for package file, excluding it from cross-file generic type lookup",
				"file", arg, "error", err)
			continue
		}
		if !util.PathExists(abs) {
			continue
		}
		files = append(files, abs)
	}
	return files
}

// loadSiblingASTs parses every .go file in the current package (via
// packageGoFiles) with ast.ParseFileFast and caches the result for the rest
// of this package's instrumentation. A file that fails to parse is logged and
// skipped rather than aborting instrumentation -- it simply isn't searched,
// the same outcome as if the declaration weren't present at all.
//
// This deliberately does NOT exclude ip.targetPath: ip.compileArgs holds the
// package's complete file set from the moment instrumentation for this
// package begins and is only ever mutated in place (writeInstrumented swaps
// an already-processed file's path for its instrumented copy, it never
// removes entries), so packageGoFiles() returns the same file set regardless
// of which file happens to be "current" when the cache is first built. Since
// resolveGenericTypeDecl is called once per package and reused via
// ip.siblingASTs, building the cache once with every file included -- and
// excluding whichever file is current at LOOKUP time instead -- is what makes
// the cache correct across the whole package, not just for the file that
// triggered the first lookup. Excluding ip.targetPath here instead would
// silently and permanently drop that file from every subsequent file's
// search once the cache is built.
func (ip *instrumentPhase) loadSiblingASTs() map[string]*dst.File {
	siblings := make(map[string]*dst.File)
	for _, file := range ip.packageGoFiles() {
		root, err := ast.ParseFileFast(file)
		if err != nil {
			ip.Warn("failed to parse package file for cross-file generic type lookup",
				"file", file, "error", err)
			continue
		}
		siblings[file] = root
	}
	return siblings
}
