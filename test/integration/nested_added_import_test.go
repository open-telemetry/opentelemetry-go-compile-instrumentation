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

	runOut := buildAndRunNestedImports(t, absoluteOtelcPath, appDir)
	assert.Contains(
		t,
		runOut,
		"nestedimports: dep rule called into late",
		"the code the dep rule injected must call into the package it added, which only links when the nested build resolved its archive",
	)
	assert.Contains(
		t,
		runOut,
		"nestedimports: late.Instrumented= true",
		"the outer build must compile late instrumented; an uninstrumented nested archive poisons their shared cache entry",
	)
}

// TestAddedImportsResolveInSecondNestedBuild covers an added import resolved
// inside a nested build. late imports inner, and the inner rule adds an import
// of leaf, so the nested build that resolves late for dep compiles inner and
// resolves leaf with a nested build of its own. The application imports leaf
// too, so the outer build links its own instrumented leaf.
//
// otelc strips -toolexec from GOFLAGS at startup. When only the outer toolexec
// added it back, the second nested build compiled leaf uninstrumented and
// inner was compiled against that copy. The outer build then reused that inner
// from their shared cache entry, and the link failed with `fingerprint
// mismatch`. -p=1 makes the outer build compile dep, and wait for its nested
// build, before it reaches inner, so the failure does not depend on timing.
func TestAddedImportsResolveInSecondNestedBuild(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	absoluteOtelcPath, err := filepath.Abs(otelcPath)
	require.NoError(t, err)
	repoRoot := filepath.Dir(absoluteOtelcPath)

	appDir := copyNestedImportsApp(t, repoRoot)

	runOut := buildAndRunNestedImports(t, absoluteOtelcPath, appDir, "GOFLAGS=-p=1")
	assert.Contains(
		t,
		runOut,
		"nestedimports: inner rule called into leaf",
		"the code the inner rule injected must call into the package it added",
	)
	assert.Contains(
		t,
		runOut,
		"nestedimports: leaf.Instrumented= true",
		"the outer build must link leaf instrumented",
	)
}

// TestAddedInternalPackageResolves covers an added import of an internal package.
// A synthetic probe outside the module cannot import dep's internal package due to
// Go's internal package visibility rules. Direct package resolution must resolve
// its archive correctly so both cold and warm builds succeed.
func TestAddedInternalPackageResolves(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	absoluteOtelcPath, err := filepath.Abs(otelcPath)
	require.NoError(t, err)
	repoRoot := filepath.Dir(absoluteOtelcPath)

	appDir := copyNestedImportsApp(t, repoRoot)

	// Create dep/internal/secret/helper/helper.go (transitive internal dependency)
	helperDir := filepath.Join(appDir, "dep", "internal", "secret", "helper")
	require.NoError(t, os.MkdirAll(helperDir, 0o755))
	helperContent := "package helper\n\nfunc SubNote() string {\n\treturn \"helper-note\"\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(helperDir, "helper.go"), []byte(helperContent), 0o644))

	// Create dep/internal/secret/secret.go importing helper
	secretDir := filepath.Join(appDir, "dep", "internal", "secret")
	require.NoError(t, os.MkdirAll(secretDir, 0o755))
	secretContent := "package secret\n\nimport \"go.opentelemetry.io/otelc/test/apps/nestedimports/dep/internal/secret/helper\"\n\nfunc Note() string {\n\treturn \"internal-secret-\" + helper.SubNote()\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(secretDir, "secret.go"), []byte(secretContent), 0o644))

	// Add function to dep/dep.go that uses strings.ToLower so our rule can wrap it
	depFile := filepath.Join(appDir, "dep", "dep.go")
	depSrc, err := os.ReadFile(depFile)
	require.NoError(t, err)
	newDepSrc := string(
		depSrc,
	) + "\nfunc SecretWork() string {\n\treturn strings.ToLower(\"from dep with secret\")\n}\n"
	require.NoError(t, os.WriteFile(depFile, []byte(newDepSrc), 0o644))

	// Call dep.SecretWork() in main.go
	mainFile := filepath.Join(appDir, "main.go")
	mainSrc, err := os.ReadFile(mainFile)
	require.NoError(t, err)
	newMainSrc := strings.Replace(
		string(mainSrc),
		`fmt.Println("nestedimports: dep=" + dep.Work())`,
		`fmt.Println("nestedimports: dep=" + dep.Work())`+"\n\t"+`fmt.Println("nestedimports: secret=" + dep.SecretWork())`,
		1,
	)
	require.NoError(t, os.WriteFile(mainFile, []byte(newMainSrc), 0o644))

	// Add rule to otelc.yaml importing dep/internal/secret
	ruleFile := filepath.Join(appDir, "instrumentation", "otelc.yaml")
	ruleSrc, err := os.ReadFile(ruleFile)
	require.NoError(t, err)
	newRule := `
dep_internal_rule:
  target: go.opentelemetry.io/otelc/test/apps/nestedimports/dep
  where:
    function_call: strings.ToLower
  do:
    - wrap_call:
        replace: "(func() string { return secret.Note() + \": \" + {{ . }} })()"
  imports:
    secret: go.opentelemetry.io/otelc/test/apps/nestedimports/dep/internal/secret
`
	require.NoError(t, os.WriteFile(ruleFile, append(ruleSrc, []byte(newRule)...), 0o644))

	goCache := filepath.Join(t.TempDir(), "gocache")
	binary := filepath.Join(appDir, "app")
	if util.IsWindows() {
		binary += ".exe"
	}

	// 1. Cold cache build
	cmd := exec.CommandContext(t.Context(), absoluteOtelcPath, "go", "build", "-o", binary, ".")
	cmd.Dir = appDir
	cmd.Env = append(
		preparedBuildBaseEnvironment(),
		"GOWORK=off",
		"GOCACHE="+goCache,
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "cold build with internal package import failed:\n%s", out)

	run := exec.CommandContext(t.Context(), binary)
	run.Dir = appDir
	runOut, err := run.CombinedOutput()
	require.NoError(t, err, string(runOut))
	assert.Contains(t, string(runOut), "nestedimports: secret=internal-secret-helper-note: from dep with secret")

	// 2. Warm cache build
	binaryWarm := filepath.Join(appDir, "app_warm")
	if util.IsWindows() {
		binaryWarm += ".exe"
	}
	cmdWarm := exec.CommandContext(t.Context(), absoluteOtelcPath, "go", "build", "-o", binaryWarm, ".")
	cmdWarm.Dir = appDir
	cmdWarm.Env = append(
		preparedBuildBaseEnvironment(),
		"GOWORK=off",
		"GOCACHE="+goCache,
	)
	outWarm, err := cmdWarm.CombinedOutput()
	require.NoError(t, err, "warm cache build with internal package import failed:\n%s", outWarm)

	runWarm := exec.CommandContext(t.Context(), binaryWarm)
	runWarm.Dir = appDir
	runWarmOut, err := runWarm.CombinedOutput()
	require.NoError(t, err, string(runWarmOut))
	assert.Contains(t, string(runWarmOut), "nestedimports: secret=internal-secret-helper-note: from dep with secret")
}

// buildAndRunNestedImports builds the application in appDir with otelc on a
// fresh GOCACHE, runs it, and returns its output. env is added to the build
// environment.
func buildAndRunNestedImports(t *testing.T, otelcPath, appDir string, env ...string) string {
	t.Helper()

	// A fresh GOCACHE makes every build cold: the nested build cannot reuse an
	// archive a warm cache already holds.
	goCache := filepath.Join(t.TempDir(), "gocache")
	binary := filepath.Join(appDir, "app")
	if util.IsWindows() {
		binary += ".exe"
	}
	cmd := exec.CommandContext(t.Context(), otelcPath, "go", "build", "-o", binary, ".")
	cmd.Dir = appDir
	cmd.Env = append(
		preparedBuildBaseEnvironment(),
		"GOWORK=off",
		"GOCACHE="+goCache,
	)
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err,
		"the link needs the archives of the added imports; an uninstrumented nested copy mismatches them:\n%s", out)

	run := exec.CommandContext(t.Context(), binary)
	run.Dir = appDir
	runOut, err := run.CombinedOutput()
	require.NoError(t, err, string(runOut))
	return string(runOut)
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
