//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

const concurrentBuildsGoMod = "module example.com/concurrentbuilds\n\ngo 1.25.0\n"

// TestConcurrentBuildsInOneModule runs otelc builds of one module at the same
// time, each with its own work dir. They all edit the go.mod of the module,
// so they must wait for each other and leave go.mod as it was.
func TestConcurrentBuildsInOneModule(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	apps := []string{"a", "b", "c"}
	env := append(os.Environ(), "GOWORK=off")

	t.Run("from different package dirs", func(t *testing.T) {
		moduleDir := concurrentBuildsModule(t, apps)
		// The default work dir of each build is the directory it starts in.
		cmds := make([]*exec.Cmd, 0, len(apps))
		for _, app := range apps {
			cmd := exec.CommandContext(t.Context(), otelcPath, "go", "build", "-o", "app", ".")
			cmd.Dir = filepath.Join(moduleDir, "cmd", app)
			cmd.Env = env
			cmds = append(cmds, cmd)
		}
		assertBuildsTookTurns(t, moduleDir, runConcurrently(t, cmds))
	})

	t.Run("with -C and separate work dirs", func(t *testing.T) {
		moduleDir := concurrentBuildsModule(t, apps)
		outside := t.TempDir()
		cmds := make([]*exec.Cmd, 0, len(apps))
		for _, app := range apps {
			cmd := exec.CommandContext(t.Context(), otelcPath, "--work-dir", t.TempDir(),
				"go", "build", "-C", moduleDir, "-o", "app-"+app, "./cmd/"+app)
			cmd.Dir = outside
			cmd.Env = env
			cmds = append(cmds, cmd)
		}
		assertBuildsTookTurns(t, moduleDir, runConcurrently(t, cmds))
	})
}

// concurrentBuildsModule writes a module with one main package per app under
// cmd/ and returns its root.
func concurrentBuildsModule(t *testing.T, apps []string) string {
	t.Helper()
	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", concurrentBuildsGoMod)
	for _, app := range apps {
		writeTestFile(t, moduleDir, filepath.Join("cmd", app, "main.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "net/http"

func main() { _, _ = http.Get("http://127.0.0.1:1/") }
`)
	}
	return moduleDir
}

// runConcurrently runs cmds at the same time and returns their combined
// output. It fails the test if one of them fails.
func runConcurrently(t *testing.T, cmds []*exec.Cmd) []string {
	t.Helper()
	outputs := make([]string, len(cmds))
	errs := make([]error, len(cmds))
	var wg sync.WaitGroup
	for i, cmd := range cmds {
		wg.Go(func() {
			out, err := cmd.CombinedOutput()
			outputs[i], errs[i] = string(out), err
		})
	}
	wg.Wait()
	for i, cmd := range cmds {
		require.NoError(t, errs[i], "%s failed:\n%s", cmd.String(), outputs[i])
	}
	return outputs
}

// assertBuildsTookTurns checks that the builds left the module as it was and
// that at least one of them waited for the build lock of another.
func assertBuildsTookTurns(t *testing.T, moduleDir string, outputs []string) {
	t.Helper()
	goMod, err := os.ReadFile(filepath.Join(moduleDir, "go.mod"))
	require.NoError(t, err)
	assert.Equal(t, concurrentBuildsGoMod, string(goMod), "go.mod must be restored")
	assert.NoFileExists(t, filepath.Join(moduleDir, "go.sum"), "go.sum must be removed")

	waited := 0
	for _, out := range outputs {
		if strings.Contains(out, "another invocation holds the build lock") {
			waited++
		}
	}
	assert.Positive(t, waited, "builds of one module must wait for each other")
}
