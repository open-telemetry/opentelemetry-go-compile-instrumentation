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
			t.Parallel()
			deps := testutil.DiscoverDirectDeps(t, appDir)
			if len(deps) == 0 {
				t.Skipf("%s has no third-party deps to bump (stdlib-only)", name)
			}
			testutil.BumpToLatest(t, appDir, deps...)
			testutil.Build(t, appDir, "go", "build", "-a")
		})
	}
}
