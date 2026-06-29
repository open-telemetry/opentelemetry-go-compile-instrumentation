// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build versionmatrix

package versionmatrix

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

// appBoundVersions returns, per test-app directory, the per-rule boundary
// versions of its instrumented direct dependencies (see testutil.BoundVersions).
// Apps with no ranged dependencies are omitted.
func appBoundVersions(t *testing.T) map[string]map[string][]string {
	appsRoot := filepath.Join("..", "apps")
	rulesRoot := filepath.Join("..", "..", "pkg", "instrumentation")
	targets := testutil.InstrumentedTargets(t, rulesRoot)

	entries, err := os.ReadDir(appsRoot)
	if err != nil {
		t.Fatalf("read %s: %v", appsRoot, err)
	}
	apps := map[string]map[string][]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		appDir := filepath.Join(appsRoot, e.Name())
		if _, statErr := os.Stat(filepath.Join(appDir, "go.mod")); statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			t.Fatalf("stat %s/go.mod: %v", appDir, statErr)
		}
		rangedDeps := testutil.DiscoverRangedDeps(t, appDir, targets)
		if bounds := testutil.BoundVersions(t, appDir, rangedDeps); len(bounds) > 0 {
			apps[appDir] = bounds
		}
	}
	return apps
}

// TestVersionMatrixTierCount logs the number of integration tiers the matrix
// needs: the largest per-rule boundary set across every instrumented
// dependency. The make target reads it to run the suite once per tier.
func TestVersionMatrixTierCount(t *testing.T) {
	tiers := 0
	for _, bounds := range appBoundVersions(t) {
		for _, versions := range bounds {
			if len(versions) > tiers {
				tiers = len(versions)
			}
		}
	}
	t.Logf("VERSIONMATRIX_TIERS=%d", tiers)
}

// TestVersionMatrixBump pins each app's instrumented direct dependencies to
// their VERSIONMATRIX_TIER-th per-rule boundary version, so the subsequent
// integration suite (phase 2 of make test-versionmatrix) exercises that tier.
// A dependency with fewer boundary versions than the tier index is left as it
// is; the integration suite still runs against it.
//
// This test only mutates test/apps/*/go.mod; the integration suite's TestMain
// pre-build rebuilds every app after the pin.
func TestVersionMatrixBump(t *testing.T) {
	tier, err := strconv.Atoi(os.Getenv("VERSIONMATRIX_TIER"))
	if err != nil {
		t.Fatalf("VERSIONMATRIX_TIER must be an integer: %v", err)
	}
	for appDir, bounds := range appBoundVersions(t) {
		pins := map[string]string{}
		for dep, versions := range bounds {
			if tier < len(versions) {
				pins[dep] = versions[tier]
			}
		}
		if len(pins) == 0 {
			continue
		}
		t.Run(filepath.Base(appDir), func(t *testing.T) {
			testutil.BumpToVersions(t, appDir, pins)
		})
	}
}
