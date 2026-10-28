// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"slices"

	"golang.org/x/tools/go/packages"

	"go.opentelemetry.io/otelc/tool/internal/pkgload"
)

// normalizeModuleDirs returns a cloned, sorted, and deduplicated copy of dirs.
// If dirs is empty, it returns an empty non-nil slice. The caller's slice is never mutated.
func normalizeModuleDirs(dirs []string) []string {
	if len(dirs) == 0 {
		return []string{}
	}
	res := slices.Clone(dirs)
	slices.Sort(res)
	return slices.Compact(res)
}

// findBuildModuleDirs returns the directories of the modules of the packages
// that "go <subcommand> <args>" builds. It does not load the import graph,
// which getBuildPackages loads for setup.
func findBuildModuleDirs(ctx context.Context, subcommand string, args []string) ([]string, error) {
	// NeedForTest keeps the test-package filter of loadBuildPackages correct,
	// and it does not stop packages.Load from using "go list -find".
	mode := packages.NeedName | packages.NeedFiles | packages.NeedModule | packages.NeedForTest
	// This load does not resolve imports, so import errors (such as a cycle)
	// surface later, in findDeps or the build.
	buildPkgs, _, err := loadBuildPackages(ctx, mode, subcommand, args)
	if err != nil {
		return nil, err
	}
	return pkgload.FindModuleDirs(ctx, buildPkgs)
}
