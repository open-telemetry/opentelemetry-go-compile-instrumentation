//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

// statsLog returns what the run's toolexec processes logged. OTELC_STATS
// makes every tool run the build executes log a "toolexec stats" line, so
// the debug log records exactly which compiles ran and which the cache
// answered.
func statsLog(t *testing.T, appDir string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(appDir, ".otelc-build", "debug.log"))
	require.NoError(t, err)
	return string(content)
}

// assertStatsCompiledPackage asserts from a stats log that a compile of pkg
// ran.
func assertStatsCompiledPackage(t *testing.T, log, pkg string) {
	t.Helper()
	assert.Contains(t, log, "package="+pkg)
}

// assertStatsCacheHitPackage asserts from a stats log that no compile of
// pkg ran: the build cache answered it, so a link that still works proves
// the warm build keeps the rule-added import.
func assertStatsCacheHitPackage(t *testing.T, log, pkg string) {
	t.Helper()
	assert.NotContains(t, log, "package="+pkg,
		"the build recompiled "+pkg+", so the run exercised cold recovery, not the cache-hit shape")
}

// TestWarmCacheKeepsRuleAddedImports covers the link step of a build whose
// compile actions all hit the build cache. A rule adds an import of extra to
// dep, and nothing else in the application imports extra, so go's own build
// graph never contains it: only the instrumented compile of dep pulls it in
// and records it for the link. On a warm GOCACHE the compile of dep is a
// cache hit, no compile records the import, and the link must still resolve
// it. This is the CI shape: a fresh checkout (no .otelc-build) combined with
// a restored GOCACHE.
func TestWarmCacheKeepsRuleAddedImports(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	appDir := t.TempDir()

	writeTestFile(t, appDir, filepath.Join("extra", "go.mod"), `module example.com/warmcache/extra

go 1.25.0
`)
	writeTestFile(t, appDir, filepath.Join("extra", "extra.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package extra

import "fmt"

// Note reports that the rule-injected code ran.
func Note() string {
	fmt.Println("extra: note called")
	return "note"
}
`)
	writeTestFile(t, appDir, filepath.Join("dep", "dep.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dep

import "strings"

func Work() string {
	return strings.ToUpper("work")
}
`)
	writeTestFile(t, appDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"example.com/warmcache/dep"
)

func main() {
	fmt.Println("warmcache: " + dep.Work())
}
`)
	writeTestFile(t, appDir, "main_test.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"example.com/warmcache/dep"
)

func TestWork(t *testing.T) {
	if got := dep.Work(); got != "WORK" {
		t.Fatalf("dep.Work() = %q, want WORK", got)
	}
}
`)
	writeTestFile(t, appDir, "go.mod", `module example.com/warmcache

go 1.25.0

require example.com/warmcache/extra v0.0.0

replace example.com/warmcache/extra => ./extra
`)
	writeTestFile(t, appDir, "rules.yml", `dep_wrap_call:
  target: example.com/warmcache/dep
  where:
    function_call: strings.ToUpper
  do:
    - wrap_call:
        replace: '(func() string { extra.Note(); return {{ . }} })()'
  imports:
    extra: example.com/warmcache/extra
`)

	// One GOCACHE for the cold and the warm build: the warm build must reuse
	// the archives the cold build stored, which is what a restored CI cache
	// provides.
	goCache := filepath.Join(t.TempDir(), "gocache")
	baseEnv := append(
		preparedBuildBaseEnvironment(),
		"GOWORK=off",
		"GOCACHE="+goCache,
		"OTELC_RULES=rules.yml",
		"OTELC_STATS=1",
	)

	runOtelcTest := func() string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), otelcPath, "go", "test", "-count=1", "-run", "TestWork", "-v", ".")
		cmd.Dir = appDir
		cmd.Env = baseEnv
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "otelc go test failed:\n%s", out)
		return string(out)
	}

	t.Run("cold build compiles and instruments", func(t *testing.T) {
		out := runOtelcTest()
		assert.Contains(t, out, "extra: note called")
		// The cold build must compile dep through toolexec, or the warm run
		// below cannot demonstrate a cache hit for it.
		assertStatsCompiledPackage(t, statsLog(t, appDir), "example.com/warmcache/dep")
	})

	t.Run("warm build after cache hit keeps the link working", func(t *testing.T) {
		// The CI shape: a fresh checkout holds no .otelc-build, so setup runs
		// from scratch while the restored GOCACHE answers every compile.
		require.NoError(t, os.RemoveAll(filepath.Join(appDir, ".otelc-build")))
		out := runOtelcTest()
		assert.Contains(t, out, "extra: note called")

		log := statsLog(t, appDir)
		// The warm run must not recompile dep: the restored cache answers
		// its instrumented compile, so no toolexec compile of it runs.
		assertStatsCacheHitPackage(t, log, "example.com/warmcache/dep")
	})
}

// TestWarmCacheKeepsRuleAddedImportsWithPGO covers a warm build under PGO.
// The cold compile of dep runs profiled, and so does the compile of the
// package the rule adds to it, which the go command builds in the same build.
// On the warm build the compile of dep is a cache hit, and the link rejects
// export data whose fingerprint differs from what the cached compile of dep
// recorded. The profile makes extra.Note hot, the one shape in which profiled
// and unprofiled compiles of a package differ.
func TestWarmCacheKeepsRuleAddedImportsWithPGO(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	appDir := t.TempDir()

	writeTestFile(t, appDir, filepath.Join("extra", "go.mod"), `module example.com/warmcache/extra

go 1.25.0
`)
	writeTestFile(t, appDir, filepath.Join("extra", "extra.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package extra

// seed is a variable, not a constant; see Note.
var seed = 1

// Note reports that the rule-injected code ran. Its body is tuned so the
// compiler cannot inline it on an ordinary build (cost past the inlining
// budget) but can inline it once the recorded profile marks it hot: the
// profiled and unprofiled compiles of this package then produce different
// export data, which is the fingerprint mismatch this test relies on.
//
// The body reads seed so the compiler cannot fold it into a constant. Folded,
// Note compiles to two instructions, and whether a CPU profiling signal ever
// lands inside them depends on the CPU: on some arm64 runners no recording
// gave Note enough samples to mark it hot. Unfolded, Note does real work on
// every call and holds most of the samples on any CPU.
func Note() int {
	x := 0
	x += seed * 1
	x += seed * 2
	x += seed * 3
	x += seed * 4
	x += seed * 5
	x += seed * 6
	x += seed * 7
	x += seed * 8
	x += seed * 9
	x += seed * 10
	x += seed * 11
	x += seed * 12
	x += seed * 13
	x += seed * 14
	x += seed * 15
	x += seed * 16
	x += seed * 17
	x += seed * 18
	x += seed * 19
	x += seed * 20
	x += seed * 21
	x += seed * 22
	x += seed * 23
	x += seed * 24
	x += seed * 25
	x += seed * 26
	x += seed * 27
	x += seed * 28
	x += seed * 29
	x += seed * 30
	x += seed * 31
	x += seed * 32
	x += seed * 33
	return x
}
`)
	writeTestFile(t, appDir, filepath.Join("dep", "dep.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dep

import "strings"

func Work() string {
	return strings.ToUpper("work")
}
`)
	writeTestFile(t, appDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"example.com/warmcache/dep"
)

func main() {
	fmt.Println("warmcache: " + dep.Work())
}
`)
	writeTestFile(t, appDir, "main_test.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"example.com/warmcache/dep"
)

func TestWork(t *testing.T) {
	if got := dep.Work(); got != "WORK" {
		t.Fatalf("dep.Work() = %q, want WORK", got)
	}
}
`)
	writeTestFile(t, appDir, "go.mod", `module example.com/warmcache

go 1.25.0

require example.com/warmcache/extra v0.0.0

replace example.com/warmcache/extra => ./extra
`)
	writeTestFile(t, appDir, "rules.yml", `dep_wrap_call:
  target: example.com/warmcache/dep
  where:
    function_call: strings.ToUpper
  do:
    - wrap_call:
        replace: '(func() string { _ = extra.Note(); return {{ . }} })()'
  imports:
    extra: example.com/warmcache/extra
`)
	writeTestFile(t, appDir, filepath.Join("profilegen", "main.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"runtime/pprof"
	"time"

	"example.com/warmcache/extra"
)

// main records a CPU profile in which extra.Note is the dominant cost. The
// inner loop does no allocation and no timing calls, so the samples land on
// Note instead of on the runtime. Profiling samples at 100 Hz, so the run
// is bounded by time, not by an iteration count: a fixed count finishes in
// about 200ms on a fast runner, and the handful of samples it leaves can
// miss Note entirely. The clock is read once per batch of 1e7 calls, so it
// costs a negligible share of the samples.
func main() {
	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		panic(err)
	}
	for start := time.Now(); time.Since(start) < time.Second; {
		for range 10_000_000 {
			_ = extra.Note()
		}
	}
	pprof.StopCPUProfile()
	if err := f.Close(); err != nil {
		panic(err)
	}
}
`)

	// Record a real CPU profile with a plain go run; the compiler rejects
	// profiles it cannot parse. Sampling is statistical: on some CI runners
	// a recording can still leave Note below the PGO hotness threshold, so
	// the recording is retried a bounded number of times until a profiled
	// compile of extra inlines Note.
	profile := filepath.Join(appDir, "cpu.pgo")
	extraDir := filepath.Join(appDir, "extra")
	assertNoteNotInlinedUnprofiled(t, extraDir)
	record := func() {
		gen := exec.CommandContext(t.Context(), "go", "run", "./profilegen", profile)
		gen.Dir = appDir
		gen.Env = append(preparedBuildBaseEnvironment(), "GOWORK=off")
		out, recErr := gen.CombinedOutput()
		require.NoError(t, recErr, "recording the CPU profile failed:\n%s", out)
	}
	profiledCompile := func() string {
		out := compileExtraDiagnostics(t, extraDir, "-pgo="+profile)
		if strings.Contains(out, noteInline) {
			return out
		}
		// On a miss, show where the samples went, so a failure tells a
		// profile that missed Note apart from a compiler that changed.
		top := exec.CommandContext(t.Context(), "go", "tool", "pprof", "-top", "-nodecount=10", profile)
		top.Env = append(preparedBuildBaseEnvironment(), "GOWORK=off")
		topOut, _ := top.CombinedOutput()
		return out + "profile:\n" + string(topOut)
	}
	attempts, profiledOut := recordUntilNoteHot(t.Logf, record, profiledCompile)
	require.Contains(
		t,
		profiledOut,
		noteInline,
		"after %d recordings a profiled compile still does not inline Note, so profiled and unprofiled extra no longer differ and the warm subtest proves nothing",
		attempts,
	)

	baseEnv := append(
		preparedBuildBaseEnvironment(),
		"GOWORK=off",
		"GOCACHE="+filepath.Join(t.TempDir(), "gocache"),
		"OTELC_RULES=rules.yml",
		"OTELC_STATS=1",
	)

	runOtelcTest := func() string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), otelcPath,
			"go", "test", "-pgo="+profile, "-count=1", "-run", "TestWork", "-v", ".")
		cmd.Dir = appDir
		cmd.Env = baseEnv
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "otelc go test failed:\n%s", out)
		return string(out)
	}

	t.Run("cold profiled build links", func(t *testing.T) {
		assert.Contains(t, runOtelcTest(), "PASS")
		// The cold build must compile dep through toolexec, or the warm run
		// below cannot demonstrate a cache hit for it.
		assertStatsCompiledPackage(t, statsLog(t, appDir), "example.com/warmcache/dep")
	})

	t.Run("warm build resolves the added import with the recorded profile", func(t *testing.T) {
		// The CI shape: a fresh checkout holds no .otelc-build, so setup runs
		// from scratch while the restored GOCACHE answers every compile.
		require.NoError(t, os.RemoveAll(filepath.Join(appDir, ".otelc-build")))
		assert.Contains(t, runOtelcTest(), "PASS")

		log := statsLog(t, appDir)
		// The warm run must not recompile dep, or it would not cover the
		// cached profiled compile.
		assertStatsCacheHitPackage(t, log, "example.com/warmcache/dep")
	})
}

// noteInline is the -m=2 diagnostic the compiler prints when it inlines
// extra.Note.
//
// The premise the warm subtest is built on: extra.Note is tuned to sit past
// the compiler's inlining budget, so only the recorded profile, which marks it
// hot, can lift it over. A compiler that inlines Note unprofiled too, or one
// the profile no longer reaches, makes the profiled and the unprofiled compile
// of extra produce the same export data: the fingerprint mismatch this test
// relies on disappears, and the warm subtest would keep passing while proving
// nothing. The compiler's -m diagnostics are an escape hatch, not a stable
// API; the wording was verified against the toolchains this repository builds
// with (go 1.26 and 1.27), and a future wording change fails loudly instead of
// silently voiding the test.
const noteInline = "can inline Note"

// compileExtraDiagnostics compiles extra with -gcflags=-m=2 plus args and
// returns the compiler output.
func compileExtraDiagnostics(t *testing.T, extraDir string, args ...string) string {
	t.Helper()
	args = append(args, "-gcflags=-m=2", ".")
	cmd := exec.CommandContext(t.Context(), "go", append([]string{"build"}, args...)...)
	cmd.Dir = extraDir
	// A fresh cache forces the compile of extra, so the diagnostics always
	// print; extra imports nothing, so this compiles one tiny package.
	cmd.Env = append(preparedBuildBaseEnvironment(),
		"GOWORK=off", "GOCACHE="+t.TempDir())
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go build %v failed:\n%s", args, out)
	return string(out)
}

// maxNoteRecordings bounds how many times recordUntilNoteHot records the
// CPU profile.
const maxNoteRecordings = 3

// recordUntilNoteHot records the profile and runs the profiled compile until
// the compile output shows Note inlined, at most maxNoteRecordings times. It
// returns the number of recordings made and the last compile output; the
// caller asserts on that output, so a run that never makes Note hot fails.
func recordUntilNoteHot(
	logf func(format string, args ...any),
	record func(),
	profiledCompile func() string,
) (int, string) {
	var out string
	for attempt := 1; attempt <= maxNoteRecordings; attempt++ {
		record()
		out = profiledCompile()
		if strings.Contains(out, noteInline) {
			return attempt, out
		}
		logf("recording %d/%d did not make Note hot:\n%s", attempt, maxNoteRecordings, out)
	}
	return maxNoteRecordings, out
}

// assertNoteNotInlinedUnprofiled fails the test when an unprofiled compile
// inlines Note, which voids the premise described at noteInline.
func assertNoteNotInlinedUnprofiled(t *testing.T, extraDir string) {
	t.Helper()
	require.NotContains(
		t,
		compileExtraDiagnostics(t, extraDir),
		noteInline,
		"an unprofiled compile inlines Note, so profiled and unprofiled extra no longer differ and the warm subtest proves nothing",
	)
}
