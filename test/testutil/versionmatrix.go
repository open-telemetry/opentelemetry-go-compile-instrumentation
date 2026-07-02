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

// publishedVersions returns the versions of dep known to the module proxy,
// as reported by "go list -m -versions".
func publishedVersions(t *testing.T, appDir, dep string) []string {
	cmd := exec.CommandContext(t.Context(), "go", "list", "-m", "-versions", dep)
	cmd.Dir = appDir
	out, err := cmd.Output()
	require.NoError(t, err, "go list -m -versions %s failed in %s", dep, appDir)

	fields := strings.Fields(string(out))
	require.NotEmpty(t, fields, "go list -m -versions %s returned no output in %s", dep, appDir)
	// The first field is the module path, the rest are published versions.
	return fields[1:]
}

// BoundVersions maps each dep to the sorted, de-duplicated set of versions the
// matrix should exercise for it: the lower and upper bound of every declared
// range, taken per rule rather than aggregated per dependency.
func BoundVersions(t *testing.T, appDir string, rangedDeps map[string][]string) map[string][]string {
	bounds := make(map[string][]string, len(rangedDeps))
	for dep, ranges := range rangedDeps {
		bounds[dep] = boundVersionSet(publishedVersions(t, appDir, dep), ranges)
	}
	return bounds
}

// boundVersionSet returns the sorted, de-duplicated bound versions for a single
// dependency: each range's lower bound (its declared start) plus its highest
// covered release. Ranges are half-open (the cap is the first unsupported
// version), so the upper bound of "v0.34.0,v0.36.0" is the newest release below
// v0.36.0. An upper bound equal to the latest release is dropped because
// LatestLibRun already exercises it and a shared failure would open a duplicate
// issue.
func boundVersionSet(versions, ranges []string) []string {
	latest := latestRelease(versions)
	set := map[string]bool{}
	for _, vr := range ranges {
		lower, _, _ := strings.Cut(vr, ",")
		set[lower] = true
		if upper := highestCovered(versions, []string{vr}); upper != "" && upper != latest {
			set[upper] = true
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	semver.Sort(out)
	return out
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

// latestRelease returns the highest plain release (no prerelease suffix)
// among versions, or "" if there is none.
func latestRelease(versions []string) string {
	latest := ""
	for _, v := range versions {
		if semver.Prerelease(v) != "" {
			continue
		}
		if latest == "" || semver.Compare(v, latest) > 0 {
			latest = v
		}
	}
	return latest
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
