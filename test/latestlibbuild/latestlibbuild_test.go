// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build latestlibbuild

package latestlibbuild

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"go.opentelemetry.io/otelc/test/testutil"
)

// shardConfig reads SHARD_INDEX/SHARD_TOTAL to split the app builds across CI
// jobs. Each app is a full `go build -a`, so a single job building every app
// exceeds the slower runners' test timeout; sharding keeps each job bounded.
// Unset (local runs) means a single shard that builds everything.
func shardConfig(t *testing.T) (index, total int) {
	t.Helper()
	total = 1
	if v := os.Getenv("SHARD_TOTAL"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("invalid SHARD_TOTAL %q: %v", v, err)
		}
		total = n
	}
	if v := os.Getenv("SHARD_INDEX"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n >= total {
			t.Fatalf("invalid SHARD_INDEX %q for SHARD_TOTAL %d", v, total)
		}
		index = n
	}
	return index, total
}

func TestLatestLibBuild(t *testing.T) {
	appsRoot := filepath.Join("..", "apps")
	rulesRoot := filepath.Join("..", "..", "instrumentation")
	targets := testutil.InstrumentedTargets(t, rulesRoot)

	shardIndex, shardTotal := shardConfig(t)

	entries, err := os.ReadDir(appsRoot)
	if err != nil {
		t.Fatalf("read %s: %v", appsRoot, err)
	}
	// appIdx counts only real app dirs (those with a go.mod) so the modulo
	// split stays balanced regardless of non-app entries. os.ReadDir returns
	// entries sorted by name, so the assignment is deterministic across jobs.
	appIdx := 0
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
		thisApp := appIdx
		appIdx++
		if thisApp%shardTotal != shardIndex {
			continue
		}
		t.Run(name, func(t *testing.T) {
			deps := testutil.DiscoverInstrumentedDeps(t, appDir, targets)
			if len(deps) == 0 {
				t.Skipf("%s has no instrumented third-party deps with supported latest versions to bump", name)
			}
			testutil.BumpToLatest(t, appDir, deps...)
			testutil.Build(t, appsRoot, name, "go", "build", "-a")
		})
	}
}
