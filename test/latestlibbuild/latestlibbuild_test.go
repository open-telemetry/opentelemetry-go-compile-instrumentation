// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build latestlibbuild

package latestlibbuild

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestLatestLibBuild(t *testing.T) {
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
			continue
		}
		t.Run(name, func(t *testing.T) {
			deps := testutil.DiscoverInstrumentedDeps(t, appDir, targets)
			if len(deps) == 0 {
				t.Skipf("%s has no instrumented third-party deps to bump", name)
			}
			testutil.BumpToLatest(t, appDir, deps...)
			testutil.Build(t, appDir, "go", "build", "-a")
		})
	}
}
