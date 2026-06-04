// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build latestlibrun

package latestlibrun

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

// TestBumpAppsToLatest bumps each test app's instrumented direct dependencies
// to @latest so that the subsequent integration suite (phase 2 of
// make test-latestlibrun) exercises the bumped go.mod files.
//
// This test intentionally performs no build or run step, it only mutates
// test/apps/*/go.mod. The integration suite's existing f.BuildAndStart(...)
// calls rebuild every app after the bump.
func TestBumpAppsToLatest(t *testing.T) {
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
		if _, err := os.Stat(filepath.Join(appDir, "go.mod")); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("stat %s/go.mod: %v", appDir, err)
		}
		t.Run(name, func(t *testing.T) {
			deps := testutil.DiscoverInstrumentedDeps(t, appDir, targets)
			if len(deps) == 0 {
				t.Skipf("%s has no instrumented third-party deps to bump", name)
			}
			testutil.BumpToLatest(t, appDir, deps...)
		})
	}
}
