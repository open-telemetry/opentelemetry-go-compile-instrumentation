// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package bench_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

// buildEnv sets GOGC=off so the Go toolchain's own GC does not add jitter
// to timed builds.
var buildEnv = append(os.Environ(), "GOGC=off")

// BenchmarkCompile runs two sub-benchmarks per scenario:
//   - BenchmarkCompile/<scenario>/plain – plain "go build -a"
//   - BenchmarkCompile/<scenario>/otelc – "otelc go build -a"
//
// Each iteration performs a full, cache-busting rebuild.
func BenchmarkCompile(b *testing.B) {
	otelcBin := requireEnv(b, "OTELC_BIN")
	scenariosDir := requireEnv(b, "BENCH_SCENARIOS_DIR")

	scenarios, err := listScenarios(scenariosDir)
	if err != nil {
		b.Fatalf("listing scenarios: %v", err)
	}

	plainArgs := buildArgs(nil)
	otelcArgs := buildArgs([]string{otelcBin})

	for _, name := range scenarios {
		dir := filepath.Join(scenariosDir, name)
		if err := runCmd(dir, "go", "mod", "download"); err != nil {
			b.Fatalf("go mod download for %s: %v", name, err)
		}

		b.Run(name+"/plain", func(b *testing.B) {
			for b.Loop() {
				if err := runCmd(dir, plainArgs...); err != nil {
					b.Fatalf("plain build: %v", err)
				}
			}
		})

		b.Run(name+"/otelc", func(b *testing.B) {
			for b.Loop() {
				if err := runCmd(dir, otelcArgs...); err != nil {
					b.Fatalf("otelc build: %v", err)
				}
			}
		})
	}
}

func listScenarios(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func buildArgs(prefix []string) []string {
	args := make([]string, 0, len(prefix)+5)
	args = append(args, prefix...)
	// -a forces a full rebuild, deliberately bypassing the cache so every
	// run reflects true end-to-end compile time.
	return append(args, "go", "build", "-a", "-o", "app", ".")
}

func runCmd(dir string, args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = buildEnv
	return cmd.Run()
}

func requireEnv(tb testing.TB, key string) string {
	tb.Helper()
	v := os.Getenv(key)
	if v == "" {
		tb.Skipf("skipping: environment variable %s is not set", key)
	}
	return v
}
