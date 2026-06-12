// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"os/exec"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// DiscoverRangedDeps returns the direct, non-replaced third-party requires
// of the go.mod at appDir that are covered by at least one instrumentation
// rule target with an explicit (non-empty) version range, mapped to the
// sorted list of those ranges.
//
// Unlike DiscoverInstrumentedDeps, deps are not filtered by whether @latest
// falls inside a declared range: a capped range whose library has already
// released past the cap is exactly what the version-matrix test must keep
// exercising.
func DiscoverRangedDeps(t *testing.T, appDir string, targets map[string][]string) map[string][]string {
	deps := map[string][]string{}
	for _, reqPath := range directNonReplacedRequires(t, appDir) {
		var ranges []string
		for vr := range findMatchingVersionRanges(reqPath, targets) {
			if vr != "" {
				ranges = append(ranges, vr)
			}
		}
		if len(ranges) > 0 {
			sort.Strings(ranges)
			deps[reqPath] = ranges
		}
	}
	return deps
}

// LowerBounds maps each dep to the smallest lower bound across its declared
// version ranges. Supported range forms are "v1.2.3" (open-ended) and
// "v1.2.3,v4.5.6" (half-open), per util.VersionInRange.
func LowerBounds(rangedDeps map[string][]string) map[string]string {
	bounds := make(map[string]string, len(rangedDeps))
	for dep, ranges := range rangedDeps {
		lowest := ""
		for _, vr := range ranges {
			lower, _, _ := strings.Cut(vr, ",")
			if lowest == "" || semver.Compare(lower, lowest) < 0 {
				lowest = lower
			}
		}
		bounds[dep] = lowest
	}
	return bounds
}

// UpperBounds maps each dep to the highest published release covered by any
// of its declared version ranges. Ranges are half-open (the cap is the first
// unsupported version), so the upper bound of "v0.34.0,v0.36.0" is the
// newest release below v0.36.0, and the upper bound of an open-ended range
// is the latest release.
func UpperBounds(t *testing.T, appDir string, rangedDeps map[string][]string) map[string]string {
	bounds := make(map[string]string, len(rangedDeps))
	for dep, ranges := range rangedDeps {
		cmd := exec.CommandContext(t.Context(), "go", "list", "-m", "-versions", dep)
		cmd.Dir = appDir
		out, err := cmd.Output()
		require.NoError(t, err, "go list -m -versions %s failed in %s", dep, appDir)

		fields := strings.Fields(string(out))
		require.NotEmpty(t, fields, "go list -m -versions %s returned no output in %s", dep, appDir)

		// The first field is the module path, the rest are published versions.
		highest := highestCovered(fields[1:], ranges)
		require.NotEmpty(t, highest,
			"no published release of %s is covered by its declared ranges %v: the ranges are incorrect",
			dep, ranges)
		bounds[dep] = highest
	}
	return bounds
}

// highestCovered returns the highest version among versions that is a plain
// release (no prerelease suffix) covered by at least one of ranges, or ""
// if there is none.
func highestCovered(versions, ranges []string) string {
	highest := ""
	for _, v := range versions {
		if semver.Prerelease(v) != "" {
			continue
		}
		if highest != "" && semver.Compare(v, highest) <= 0 {
			continue
		}
		for _, vr := range ranges {
			if util.VersionInRange(v, vr) {
				highest = v
				break
			}
		}
	}
	return highest
}

// BumpToVersions runs "go get <dep>@<version>" for each dep in appDir,
// followed by "go mod tidy", then verifies that each dep resolved to exactly
// the requested version. A higher resolved version means another module in
// the build graph requires more than the declared bound, so the bound cannot
// be exercised as declared and the rule's version range needs fixing.
// CAUTION: The go.mod and go.sum files in appDir are modified in place.
func BumpToVersions(t *testing.T, appDir string, depVersions map[string]string) {
	deps := make([]string, 0, len(depVersions))
	for dep := range depVersions {
		deps = append(deps, dep)
	}
	sort.Strings(deps)

	for _, dep := range deps {
		version := depVersions[dep]
		t.Logf("pinning %s in %s to %s", dep, appDir, version)
		cmd := exec.CommandContext(t.Context(), "go", "get", dep+"@"+version) //nolint:gosec // input from rule files
		cmd.Dir = appDir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "go get %s@%s failed in %s:\n%s", dep, version, appDir, string(out))
	}

	tidyCmd := exec.CommandContext(t.Context(), "go", "mod", "tidy")
	tidyCmd.Dir = appDir
	tidyOut, err := tidyCmd.CombinedOutput()
	require.NoError(t, err, "go mod tidy failed in %s:\n%s", appDir, string(tidyOut))

	for _, dep := range deps {
		version := depVersions[dep]
		cmd := exec.CommandContext(t.Context(), "go", "list", "-m", "-f", "{{.Version}}", dep)
		cmd.Dir = appDir
		listOut, listErr := cmd.Output()
		require.NoError(t, listErr, "go list -m -f {{.Version}} %s failed in %s", dep, appDir)

		resolved := strings.TrimSpace(string(listOut))
		require.Equal(t, version, resolved,
			"%s resolved to %s instead of the declared bound %s in %s: "+
				"another module in the build graph forces a different version, "+
				"so this bound cannot be exercised; fix the rule's version range "+
				"or the conflicting requirement",
			dep, resolved, version, appDir)
	}
}
