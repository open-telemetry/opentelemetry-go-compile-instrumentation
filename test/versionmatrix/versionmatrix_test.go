// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build versionmatrix

package versionmatrix

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

// TestBumpAppsToLowerBound pins each test app's instrumented direct
// dependencies to the lowest lower bound declared by the matching
// instrumentation rules, so that the subsequent integration suite (phase 2
// of make test-versionmatrix) exercises the floor of each library's
// declared support window end to end.
func TestBumpAppsToLowerBound(t *testing.T) {
	bumpApps(t, func(_ *testing.T, _ string, rangedDeps map[string][]string) map[string]string {
		return testutil.LowerBounds(rangedDeps)
	})
}

// TestBumpAppsToUpperBound pins each test app's instrumented direct
// dependencies to the highest published release covered by the matching
// rules' version ranges. For a capped range that is the newest release below
// the cap; for an open-ended range it is the latest release, deliberately
// overlapping LatestLibRun.
func TestBumpAppsToUpperBound(t *testing.T) {
	bumpApps(t, testutil.UpperBounds)
}

// bumpApps pins the instrumented direct dependencies of every app under
// test/apps to the version selected by bounds. It intentionally performs no
// build or run step, it only mutates test/apps/*/go.mod; the integration
// suite's TestMain pre-build rebuilds every app after the pin.
func bumpApps(t *testing.T, bounds func(*testing.T, string, map[string][]string) map[string]string) {
	appsRoot := filepath.Join("..", "apps")
	rulesRoot := filepath.Join("..", "..", "pkg", "instrumentation")
	targets := testutil.InstrumentedTargets(t, rulesRoot)

	entries, err := os.ReadDir(appsRoot)
	if err != nil {
		t.Fatalf("read %s: %v", appsRoot, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		appDir := filepath.Join(appsRoot, name)
		if _, statErr := os.Stat(filepath.Join(appDir, "go.mod")); statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			t.Fatalf("stat %s/go.mod: %v", appDir, statErr)
		}
		t.Run(name, func(t *testing.T) {
			rangedDeps := testutil.DiscoverRangedDeps(t, appDir, targets)
			if len(rangedDeps) == 0 {
				t.Skipf("%s has no instrumented deps with a declared version range", name)
			}
			testutil.BumpToVersions(t, appDir, bounds(t, appDir, rangedDeps))
		})
	}
}
