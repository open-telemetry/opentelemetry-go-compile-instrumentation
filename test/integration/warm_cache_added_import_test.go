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
	)

	runOtelcTest := func(env []string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), otelcPath, "go", "test", "-count=1", "-run", "TestWork", "-v", ".")
		cmd.Dir = appDir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "otelc go test failed:\n%s", out)
		return string(out)
	}

	t.Run("cold build compiles and instruments", func(t *testing.T) {
		out := runOtelcTest(baseEnv)
		assert.Contains(t, out, "extra: note called")
	})

	t.Run("warm build after cache hit keeps the link working", func(t *testing.T) {
		// The CI shape: a fresh checkout holds no .otelc-build, so setup runs
		// from scratch while the restored GOCACHE answers every compile.
		require.NoError(t, os.RemoveAll(filepath.Join(appDir, ".otelc-build")))
		out := runOtelcTest(baseEnv)
		assert.Contains(t, out, "extra: note called")
	})
}
