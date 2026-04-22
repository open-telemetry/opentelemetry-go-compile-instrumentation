// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build overhead_check

// This file enforces an absolute upper bound on otelc's compile-time overhead.
// It is excluded from normal test runs via the overhead_check build tag and is
// invoked by the CI threshold job:
//
//	go test -tags=overhead_check -run=TestOverheadCeiling
//
// Required environment variables:
//
//	OTELC_BIN              – absolute path to the otelc binary
//	BENCH_SCENARIOS_DIR    – absolute path to the scenarios directory
//	BENCH_MAX_OVERHEAD_PCT – maximum allowed overhead percentage (default 150)
//
// Per scenario it runs 1 warmup + timedRuns timed builds with each tool,
// taking the minimum as the representative time. Min-of-N is correct here:
// builds cannot finish faster than their true cost, so slow outliers are noise.

package bench_test

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// timedRuns is the number of timed builds per tool per scenario.
const timedRuns = 3

func TestOverheadCeiling(t *testing.T) {
	otelcBin := requireEnv(t, "OTELC_BIN")
	scenariosDir := requireEnv(t, "BENCH_SCENARIOS_DIR")
	maxPct := floatEnv(t, "BENCH_MAX_OVERHEAD_PCT", 150.0)

	scenarios, err := listScenarios(scenariosDir)
	if err != nil {
		t.Fatalf("listing scenarios: %v", err)
	}

	plainArgs := buildArgs(nil)
	otelcArgs := buildArgs([]string{otelcBin})

	for _, name := range scenarios {
		dir := filepath.Join(scenariosDir, name)
		if err := runCmd(dir, "go", "mod", "download"); err != nil {
			t.Fatalf("go mod download for %s: %v", name, err)
		}

		// Warmup once with each tool so caches and FS buffers are stable.
		if err := runCmd(dir, plainArgs...); err != nil {
			t.Fatalf("warmup plain for %s: %v", name, err)
		}
		if err := runCmd(dir, otelcArgs...); err != nil {
			t.Fatalf("warmup otelc for %s: %v", name, err)
		}

		plain := minBuildTime(t, dir, plainArgs, name)
		otelcTime := minBuildTime(t, dir, otelcArgs, name)
		pct := (otelcTime - plain) / plain * 100

		t.Logf("%s: plain=%.3fs  otelc=%.3fs  overhead=%+.1f%%", name, plain, otelcTime, pct)

		if pct > maxPct {
			t.Errorf("%s: overhead %.1f%% exceeds ceiling %.1f%%", name, pct, maxPct)
		}
	}
}

// minBuildTime runs the build timedRuns times and returns the smallest
// elapsed seconds.
func minBuildTime(t *testing.T, dir string, args []string, name string) float64 {
	t.Helper()
	best := math.MaxFloat64
	for range timedRuns {
		start := time.Now()
		if err := runCmd(dir, args...); err != nil {
			t.Fatalf("timed build for %s: %v", name, err)
		}
		if elapsed := time.Since(start).Seconds(); elapsed < best {
			best = elapsed
		}
	}
	return best
}

// floatEnv parses key as a float64
func floatEnv(t *testing.T, key string, defaultVal float64) float64 {
	t.Helper()
	s := os.Getenv(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("parsing %s=%q: %v", key, s, err)
	}
	return v
}
