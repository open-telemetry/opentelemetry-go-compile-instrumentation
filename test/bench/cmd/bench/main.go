// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Command bench measures compile-time overhead introduced by otelc.
//
// For each benchmark scenario it runs both a plain "go build -a" and an
// "otelc go build -a", alternating between the two on each iteration so that
// both tools experience similar system conditions. Results are emitted as JSON.
//
// Usage:
//
//	bench -otelc=./otelc -scenarios=../../scenarios -iterations=5 -warmup=1 -output=bench.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"gonum.org/v1/gonum/stat"
)

// ScenarioResult holds the comparison between plain go build and otelc for a
// single benchmark scenario.
type ScenarioResult struct {
	Scenario    string  `json:"scenario"`
	Iterations  int     `json:"iterations"`
	Warmup      int     `json:"warmup"`
	PlainMean   float64 `json:"plain_mean_s"`
	PlainRange  float64 `json:"plain_range_s"`
	OtelcMean   float64 `json:"otelc_mean_s"`
	OtelcRange  float64 `json:"otelc_range_s"`
	OverheadPct float64 `json:"overhead_pct"`
}

// buildEnv is the environment used for all build commands.
// GOGC=off suppresses GC jitter inside the Go toolchain during timed builds.
var buildEnv = append(os.Environ(), "GOGC=off")

func main() {
	otelcBin := flag.String("otelc", "./otelc", "Path to the otelc binary")
	scenariosDir := flag.String("scenarios", "test/bench/scenarios", "Directory containing benchmark scenario modules")
	iterations := flag.Int("iterations", 5, "Number of timed build repetitions per scenario and tool")
	warmup := flag.Int("warmup", 1, "Number of discarded warmup builds before timing begins")
	outputFile := flag.String("output", "bench.json", "Output file path for benchmark results JSON")
	maxOverheadPct := flag.Float64("max-overhead-pct", -1, "Fail if any scenario's otelc overhead exceeds this percentage relative to the plain baseline (negative = disabled)")
	flag.Parse()

	otelcAbs, err := filepath.Abs(*otelcBin)
	if err != nil {
		log.Fatalf("resolving otelc path: %v", err)
	}

	scenariosAbs, err := filepath.Abs(*scenariosDir)
	if err != nil {
		log.Fatalf("resolving scenarios dir: %v", err)
	}

	scenarios, err := listScenarios(scenariosAbs)
	if err != nil {
		log.Fatalf("listing scenarios: %v", err)
	}

	log.Printf("found %d scenarios: %v", len(scenarios), scenarios)
	log.Printf("running %d warmup + %d timed iterations per scenario/tool", *warmup, *iterations)

	var results []ScenarioResult
	var violations []string

	for _, name := range scenarios {
		dir := filepath.Join(scenariosAbs, name)

		plainArgs := buildArgs(nil)
		otelcArgs := buildArgs([]string{otelcAbs})

		plainTimes, otelcTimes, err := measureInterleaved(dir, plainArgs, otelcArgs, *warmup, *iterations)
		if err != nil {
			log.Fatalf("scenario %s failed: %v", name, err)
		}

		plainMean := stat.Mean(plainTimes, nil)
		otelcMean := stat.Mean(otelcTimes, nil)

		overheadPct := 0.0
		if plainMean > 0 {
			overheadPct = (otelcMean - plainMean) / plainMean * 100
		}

		results = append(results, ScenarioResult{
			Scenario:    name,
			Iterations:  *iterations,
			Warmup:      *warmup,
			PlainMean:   round3(plainMean),
			PlainRange:  round3(trimmedStddev(plainTimes)),
			OtelcMean:   round3(otelcMean),
			OtelcRange:  round3(trimmedStddev(otelcTimes)),
			OverheadPct: round3(overheadPct),
		})

		log.Printf("scenario=%-12s  plain=%.3fs (±%.3f)  otelc=%.3fs (±%.3f)  overhead=%+.1f%%",
			name, plainMean, trimmedStddev(plainTimes), otelcMean, trimmedStddev(otelcTimes), overheadPct)

		if *maxOverheadPct >= 0 && overheadPct > *maxOverheadPct {
			violations = append(violations,
				fmt.Sprintf("  scenario=%-12s  overhead=%.1f%%  threshold=%.1f%%", name, overheadPct, *maxOverheadPct))
		}
	}

	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		log.Fatalf("marshaling results: %v", err)
	}

	if err := os.WriteFile(*outputFile, out, 0o644); err != nil {
		log.Fatalf("writing output file %s: %v", *outputFile, err)
	}

	log.Printf("results written to %s", *outputFile)

	if len(violations) > 0 {
		log.Printf("FAIL: overhead threshold of %.1f%% exceeded in %d scenario(s):", *maxOverheadPct, len(violations))
		for _, v := range violations {
			log.Print(v)
		}
		os.Exit(1)
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
	// -a forces a full rebuild from scratch, deliberately bypassing the build
	// cache so every run reflects the true end-to-end compile time of all
	// packages rather than a cache-warmed incremental build.
	return append(args, "go", "build", "-a", "-o", "app", ".")
}

// measureInterleaved downloads module dependencies, runs warmup builds for both
// tools, then times plainArgs and otelcArgs in alternating order so that both
// tools experience the same system conditions within each iteration cycle.
func measureInterleaved(dir string, plainArgs, otelcArgs []string, warmup, iterations int) ([]float64, []float64, error) {
	if err := runCmd(dir, "go", "mod", "download"); err != nil {
		return nil, nil, fmt.Errorf("go mod download: %w", err)
	}

	for range warmup {
		if err := runCmd(dir, plainArgs...); err != nil {
			return nil, nil, fmt.Errorf("warmup plain: %w", err)
		}
		if err := runCmd(dir, otelcArgs...); err != nil {
			return nil, nil, fmt.Errorf("warmup otelc: %w", err)
		}
	}

	plainTimes := make([]float64, 0, iterations)
	otelcTimes := make([]float64, 0, iterations)

	for range iterations {
		pt, err := timeRun(dir, plainArgs)
		if err != nil {
			return nil, nil, fmt.Errorf("plain build: %w", err)
		}
		plainTimes = append(plainTimes, pt)

		tr, err := timeRun(dir, otelcArgs)
		if err != nil {
			return nil, nil, fmt.Errorf("otelc build: %w", err)
		}
		otelcTimes = append(otelcTimes, tr)
	}

	return plainTimes, otelcTimes, nil
}

// timeRun executes args in dir and returns wall-clock duration in seconds.
func timeRun(dir string, args []string) (float64, error) {
	start := time.Now()
	if err := runCmd(dir, args...); err != nil {
		return 0, err
	}
	return time.Since(start).Seconds(), nil
}

// runCmd executes a command in the given directory using buildEnv.
func runCmd(dir string, args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = buildEnv
	return cmd.Run()
}

// trimmedStddev returns the population standard deviation after dropping the
// single fastest and slowest sample. With fewer than five samples it falls back
// to the full-sample standard deviation to avoid an empty set.
func trimmedStddev(values []float64) float64 {
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	trimmed := sorted
	if len(sorted) >= 5 {
		trimmed = sorted[1 : len(sorted)-1]
	}

	return stat.PopStdDev(trimmed, nil)
}

// round3 rounds v to 3 decimal places.
func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}
