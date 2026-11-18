//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestUnlockedBuildsReuseCompilerCache(t *testing.T) {
	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelc, err = filepath.Abs(otelc)
	require.NoError(t, err)
	module, expectedGoMod := concurrentBuildsModule(t, []string{"a"})
	binary := filepath.Join(t.TempDir(), "app")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	env := append(preparedBuildBaseEnvironment(), "GOWORK=off",
		"GOCACHE="+t.TempDir(), "OTELC_UNLOCKED_COMPILE=1", "OTELC_STATS=1", "OTEL_SDK_DISABLED=true")
	build := func() string {
		t.Helper()
		require.NoError(t, os.RemoveAll(filepath.Join(module, ".otelc-build")))
		runOtelcCommand(t, module, env, otelc, "go", "build", "-o", binary, "./cmd/a")
		return statsLog(t, module)
	}
	cold := build()
	assertStatsCompiledPackage(t, cold, "runtime")
	assertStatsCompiledPackage(t, cold, "net/http")
	warm := build()
	assertStatsCacheHitPackage(t, warm, "runtime")
	assertStatsCacheHitPackage(t, warm, "net/http")
	source := filepath.Join(module, "cmd", "a", "main.go")
	content, err := os.ReadFile(source)
	require.NoError(t, err)

	writeTestFile(t, module, filepath.Join("cmd", "a", "main.go"),
		strings.Replace(string(content), "func main() {", "func main() { println(\"updated\");", 1))

	build()
	cmd := exec.CommandContext(t.Context(), binary)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	assert.Contains(t, string(out), "updated", "a warm build must still see source edits")
	goMod, err := os.ReadFile(filepath.Join(module, "go.mod"))
	require.NoError(t, err)
	assert.Equal(t, expectedGoMod, string(goMod))
	assert.NoFileExists(t, filepath.Join(module, "go.sum"))
}

// The go wrapper pauses at compile, not setup. Both builds must reach the
// barrier before release, proving the module lock does not cover compilation.
func TestUnlockedBuildsSurviveCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("compile barrier requires a POSIX shell")
	}
	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelc, err = filepath.Abs(otelc)
	require.NoError(t, err)
	goPath, err := exec.LookPath("go")
	require.NoError(t, err)
	root := t.TempDir()
	module, expectedGoMod := concurrentBuildsModule(t, []string{"a", "b"})
	writeTestFile(t, module, "root.go", "package concurrentbuilds\nimport _ \"net/http\"\n")
	wrapperDir := filepath.Join(root, "bin")
	writeTestFile(t, wrapperDir, "go", fmt.Sprintf(`#!/bin/sh
case "$*" in
  *-toolexec*)
    touch "$OTELC_TEST_BARRIER/ready-$OTELC_TEST_BUILD"
    while [ ! -f "$OTELC_TEST_BARRIER/release" ]; do sleep 0.1; done
    ;;
esac
exec %q "$@"
`, goPath))
	require.NoError(t, os.Chmod(filepath.Join(wrapperDir, "go"), 0o755))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	env := append(os.Environ(), "GOWORK=off", "OTELC_UNLOCKED_COMPILE=1",
		"PATH="+wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"OTELC_TEST_BARRIER="+root)
	cmds := make([]*exec.Cmd, 0, 2)
	for _, app := range []string{"a", "b"} {
		output := filepath.Join(root, app)
		cmd := exec.CommandContext(ctx, otelc, "go", "build", "-race", "-o", output, "./cmd/"+app)
		cmd.Dir = module
		cmd.Env = append(append([]string{}, env...), "OTELC_TEST_BUILD="+app)
		cmd.WaitDelay = 5 * time.Second
		cmds = append(cmds, cmd)
	}
	type result struct {
		outputs []string
		errs    []error
	}
	finished := make(chan result, 1)
	go func() {
		outputs, errs := startConcurrently(cmds)
		finished <- result{outputs: outputs, errs: errs}
	}()
	// Release on assertion failure too, so no descendant compiler is stranded.
	defer func() { _ = os.WriteFile(filepath.Join(root, "release"), nil, 0o600) }()
	require.Eventually(t, func() bool {
		for _, app := range []string{"a", "b"} {
			if _, statErr := os.Stat(filepath.Join(root, "ready-"+app)); statErr != nil {
				return false
			}
		}
		return true
	}, 3*time.Minute, 50*time.Millisecond, "both compilers must overlap outside the lock")

	// Re-extract the shared bundle, then remove it while both compilers wait.
	for _, args := range [][]string{{"cleanup"}, {"setup"}, {"cleanup"}} {
		cmd := exec.CommandContext(ctx, otelc, args...)
		cmd.Dir = module
		cmd.Env = append(os.Environ(), "GOWORK=off", "OTELC_UNLOCKED_COMPILE=0")
		out, commandErr := cmd.CombinedOutput()
		require.NoError(t, commandErr, "%s: %s", args, out)
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "release"), nil, 0o600))
	select {
	case outcome := <-finished:
		for index, buildErr := range outcome.errs {
			require.NoError(t, buildErr, "%s", outcome.outputs[index])
		}
	case <-ctx.Done():
		t.Fatal("overlapping builds did not complete:", ctx.Err())
	}
	goMod, err := os.ReadFile(filepath.Join(module, "go.mod"))
	require.NoError(t, err)
	assert.Equal(t, expectedGoMod, string(goMod))
	assert.NoFileExists(t, filepath.Join(module, "go.sum"))
	for _, app := range []string{"a", "b"} {
		binary := filepath.Join(root, app)
		out, commandErr := exec.CommandContext(ctx, goPath, "tool", "nm", binary).CombinedOutput()
		require.NoError(t, commandErr, "%s", out)
		assert.Contains(t, string(out), "otelc", "binary must contain instrumentation")
		cmd := exec.CommandContext(ctx, binary)
		cmd.Env = append(os.Environ(), "OTEL_SDK_DISABLED=true")
		out, commandErr = cmd.CombinedOutput()
		require.NoError(t, commandErr, "%s", out)
		assert.NotContains(t, string(out), "DATA RACE")
	}
}
