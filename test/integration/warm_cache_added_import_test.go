//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"os"
	"os/exec"
	"path/filepath"
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
// the warm-cache recovery, not the compile-side tracking.
func assertStatsCacheHitPackage(t *testing.T, log, pkg string) {
	t.Helper()
	assert.NotContains(t, log, "package="+pkg,
		"the build recompiled "+pkg+", so the run exercised cold recovery, not the cache-hit shape")
}

// assertStatsLinkRecovered asserts from a stats log that the link updated
// its importcfg: the run holds no compile tracking file, so only the
// matched.json recovery in the link can have supplied the imports.
func assertStatsLinkRecovered(t *testing.T, log string) {
	t.Helper()
	assert.Contains(t, log, "Updated link importcfg",
		"the link ran no importcfg update; the rule-added import was never recovered")
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
		// The fresh .otelc-build starts with no tracking files, so the only
		// way the link importcfg gained the rule-added import is the
		// matched.json recovery in the link phase.
		assertStatsLinkRecovered(t, log)
	})
}

// TestWarmCacheKeepsRuleAddedImportsWithPGO covers the warm-cache link
// recovery under a PGO build. The cold compile of dep runs profiled, and so
// does the nested build that resolves the import the rule adds to it. On the
// warm build the compile of dep is a cache hit, so only the link resolves the
// import, and the archives it resolves must be compiled with the same
// profile: the link rejects export data whose fingerprint differs from what
// the cached compile of dep recorded. The profile makes extra.Note hot, the
// one shape in which profiled and unprofiled compiles of a package differ.
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

// Note reports that the rule-injected code ran. Its body is tuned so the
// compiler cannot inline it on an ordinary build (cost past the inlining
// budget) but can inline it once the recorded profile marks it hot: the
// profiled and unprofiled compiles of this package then produce different
// export data, which is the fingerprint mismatch this test relies on.
func Note() int {
	x := 0
	x += 1 * 1
	x += 2 * 2
	x += 3 * 3
	x += 4 * 4
	x += 5 * 5
	x += 6 * 6
	x += 7 * 7
	x += 8 * 8
	x += 9 * 9
	x += 10 * 10
	x += 11 * 11
	x += 12 * 12
	x += 13 * 13
	x += 14 * 14
	x += 15 * 15
	x += 16 * 16
	x += 17 * 17
	x += 18 * 18
	x += 19 * 19
	x += 20 * 20
	x += 21 * 21
	x += 22 * 22
	x += 23 * 23
	x += 24 * 24
	x += 25 * 25
	x += 26 * 26
	x += 27 * 27
	x += 28 * 28
	x += 29 * 29
	x += 30 * 30
	x += 31 * 31
	x += 32 * 32
	x += 33 * 33
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

	"example.com/warmcache/extra"
)

// main records a CPU profile in which extra.Note is the dominant cost. The
// loop does no allocation and no timing calls, so the samples land on Note
// instead of on the runtime.
func main() {
	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		panic(err)
	}
	for i := 0; i < 1000000000; i++ {
		_ = extra.Note()
	}
	pprof.StopCPUProfile()
	if err := f.Close(); err != nil {
		panic(err)
	}
}
`)

	// Record a real CPU profile with a plain go run; the compiler rejects
	// profiles it cannot parse.
	profile := filepath.Join(appDir, "cpu.pgo")
	gen := exec.CommandContext(t.Context(), "go", "run", "./profilegen", profile)
	gen.Dir = appDir
	gen.Env = append(preparedBuildBaseEnvironment(), "GOWORK=off")
	out, err := gen.CombinedOutput()
	require.NoError(t, err, "recording the CPU profile failed:\n%s", out)

	// The profile path must be absolute: the nested builds toolexec runs
	// for the link resolution work from other directories.
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
		// The warm run must not recompile dep: with a compile of dep the
		// tracking files would supply the profiled archive and the run would
		// not prove the link-side profile.
		assertStatsCacheHitPackage(t, log, "example.com/warmcache/dep")
		// The fresh .otelc-build holds no tracking files, so only the link
		// recovery, run with the recorded profile, can have resolved extra.
		assertStatsLinkRecovered(t, log)
	})
}
