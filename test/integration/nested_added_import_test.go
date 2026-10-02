// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
	"go.opentelemetry.io/otelc/tool/util"
)

// The late package is padded with trivial functions so that instrumenting it
// takes clearly longer than the nested go list needs to reach its own compile
// of it. Without padding, the outer build can finish late first and a build
// that skips instrumentation in the nested build only fails some of the time.
const (
	nestedImportPadFiles = 4
	nestedImportPadFuncs = 640
)

// TestAddedImportsResolveInNestedBuild covers the nested `go list -export` that
// updateImportConfig runs when a rule adds an import the target package did not
// have. The dep rule adds an import of late, a package the application itself
// also imports, so the outer build compiles late too, after the nested build
// resolved it for dep.
//
// Both builds answer `-V=full` with the same otelc marker, so they share cache
// keys and must produce the same instrumented archive. When a nested build
// skipped instrumentation, a cold GOCACHE made it compile late uninstrumented:
// the outer build then linked its own instrumented archive while dep
// fingerprinted the nested copy, and the link failed with `fingerprint
// mismatch`. When the timings aligned the other way, the outer build reused the
// uninstrumented nested archive instead, and late linked without its rule
// applied. Padding late rules out the fast outer build, so both failure shapes
// turn deterministic.
func TestAddedImportsResolveInNestedBuild(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	absoluteOtelcPath, err := filepath.Abs(otelcPath)
	require.NoError(t, err)
	repoRoot := filepath.Dir(absoluteOtelcPath)

	appDir := copyNestedImportsApp(t, repoRoot)
	padNestedImportsLate(t, appDir)

	// A fresh GOCACHE makes every build cold: the nested build cannot reuse an
	// archive a warm cache already holds.
	goCache := filepath.Join(t.TempDir(), "gocache")
	binary := filepath.Join(appDir, "app")
	if util.IsWindows() {
		binary += ".exe"
	}
	cmd := exec.CommandContext(t.Context(), absoluteOtelcPath, "go", "build", "-o", binary, ".")
	cmd.Dir = appDir
	cmd.Env = append(
		preparedBuildBaseEnvironment(),
		"GOWORK=off",
		"GOCACHE="+goCache,
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err,
		"the link needs the archive dep imported; an uninstrumented nested copy mismatches it:\n%s", out)

	run := exec.CommandContext(t.Context(), binary)
	run.Dir = appDir
	runOut, err := run.CombinedOutput()
	require.NoError(t, err, string(runOut))
	assert.Contains(
		t,
		string(runOut),
		"nestedimports: dep rule called into late",
		"the code the dep rule injected must call into the package it added, which only links when the nested build resolved its archive",
	)
	assert.Contains(
		t,
		string(runOut),
		"nestedimports: late.Instrumented= true",
		"the outer build must compile late instrumented; an uninstrumented nested archive poisons their shared cache entry",
	)
}

// copyNestedImportsApp copies the committed nestedimports application into a
// scratch directory and points its replaces at the repository, so the test can
// extend the copy without touching the committed tree.
func copyNestedImportsApp(t *testing.T, repoRoot string) string {
	t.Helper()

	source, err := filepath.Abs(filepath.Join("..", "apps", "nestedimports"))
	require.NoError(t, err)
	appDir := filepath.Join(t.TempDir(), "nestedimports")
	require.NoError(t, os.CopyFS(appDir, os.DirFS(source)))

	// Drop leftovers of local builds in the committed tree; this test always
	// builds from scratch.
	require.NoError(t, os.RemoveAll(filepath.Join(appDir, util.BuildTempDir)))
	if err := os.Remove(filepath.Join(appDir, util.BuildLockFile)); err != nil && !os.IsNotExist(err) {
		require.NoError(t, err)
	}
	for _, name := range []string{"app", "app.exe"} {
		_ = os.Remove(filepath.Join(appDir, name))
	}

	// The committed go.mod replaces the otelc modules with paths relative to
	// the application; rewrite them to the repository this test runs from.
	goModPath := filepath.Join(appDir, "go.mod")
	raw, err := os.ReadFile(goModPath)
	require.NoError(t, err)
	goMod := string(raw)
	// Replace the longest relative path first: ../../.. is a prefix of the
	// others, and the results would nest otherwise.
	for _, rel := range []string{"../../../pkg/runtime", "../../../pkg", "../../.."} {
		target := filepath.Join(repoRoot, strings.TrimPrefix(rel, "../../.."))
		goMod = strings.ReplaceAll(goMod, rel, strconv.Quote(target))
	}
	require.NoError(t, os.WriteFile(goModPath, []byte(goMod), 0o644))

	return appDir
}

// padNestedImportsLate writes trivial functions into the copy's late package.
func padNestedImportsLate(t *testing.T, appDir string) {
	t.Helper()

	lateDir := filepath.Join(appDir, "late")
	for f := range nestedImportPadFiles {
		var b strings.Builder
		b.WriteString("// Copyright The OpenTelemetry Authors\n")
		b.WriteString("// SPDX-License-Identifier: Apache-2.0\n\n")
		b.WriteString("package late\n\n")
		for i := range nestedImportPadFuncs {
			fmt.Fprintf(&b, "func Pad%d_%d(x int) int { y := x * %d; _ = y; return x + %d }\n", f, i, i+2, i+2)
		}
		name := filepath.Join(lateDir, fmt.Sprintf("late_pad%d.go", f))
		require.NoError(t, os.WriteFile(name, []byte(b.String()), 0o644))
	}
}
