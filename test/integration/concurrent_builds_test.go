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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

const concurrentBuildsGoMod = "module example.com/concurrentbuilds\n\ngo 1.25.0\n"

// envCrossModuleOtelc gives the tests of the cross-module test modules the
// path of the otelc binary.
const envCrossModuleOtelc = "OTELC_CROSS_MODULE_TEST_OTELC"

// crossModuleTestSource is a test in module %[1]s that runs an otelc build
// of the cmd/app package of the sibling module %[2]s.
const crossModuleTestSource = `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package %[1]s

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBuildOtherModule(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), os.Getenv("` + envCrossModuleOtelc + `"),
		"--work-dir", t.TempDir(), "go", "build", "-o", filepath.Join(t.TempDir(), "app"), "./cmd/app")
	cmd.Dir = filepath.Join("..", "%[2]s")
	out, err := cmd.CombinedOutput()
	t.Logf("otelc build of module %[2]s:\n%%s", out)
	if err != nil {
		t.Fatalf("otelc build of module %[2]s failed: %%v", err)
	}
}
`

// TestNestedBuildsAcrossModules runs otelc go test in two modules at the same
// time. A test in each module runs an otelc build of the other module, so each
// nested build waits for the module that the other otelc go test holds. One of
// the nested builds must fail with a deadlock error, so that the other one
// goes on, instead of both waiting until the test timeout.
func TestNestedBuildsAcrossModules(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	root := t.TempDir()
	modules := []string{"a", "b"}
	for i, name := range modules {
		dir := filepath.Join(root, name)
		writeTestFile(t, dir, "go.mod", fmt.Sprintf("module example.com/%s\n\ngo 1.25.0\n", name))
		writeTestFile(t, dir, filepath.Join("cmd", "app", "main.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "net/http"

func main() { _, _ = http.Get("http://127.0.0.1:1/") }
`)
		writeTestFile(t, dir, name+"_test.go", fmt.Sprintf(crossModuleTestSource, name, modules[1-i]))
	}

	// The go test -timeout ends a deadlocked run; the context is a backstop.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	env := append(os.Environ(), "GOWORK=off", envCrossModuleOtelc+"="+otelcPath)
	cmds := make([]*exec.Cmd, 0, len(modules))
	for _, name := range modules {
		cmd := exec.CommandContext(ctx, otelcPath, "go", "test", "-count=1", "-timeout=2m", "-v", ".")
		cmd.Dir = filepath.Join(root, name)
		cmd.Env = env
		cmd.WaitDelay = 10 * time.Second
		cmds = append(cmds, cmd)
	}
	outputs, errs := startConcurrently(cmds)

	var failed []string
	for i, name := range modules {
		if errs[i] != nil {
			failed = append(failed, name)
			assert.Contains(t, outputs[i], "would deadlock",
				"otelc go test in module %s must fail only with the deadlock error:\n%s", name, outputs[i])
		}
	}
	assert.Len(t, failed, 1, "exactly one otelc go test must fail:\n%s", strings.Join(outputs, "\n"))

	for _, name := range modules {
		goMod, readErr := os.ReadFile(filepath.Join(root, name, "go.mod"))
		require.NoError(t, readErr)
		assert.Equal(t, fmt.Sprintf("module example.com/%s\n\ngo 1.25.0\n", name), string(goMod),
			"go.mod of module %s must be restored", name)
	}
}

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

// startConcurrently runs cmds at the same time and returns their combined
// output and errors.
func startConcurrently(cmds []*exec.Cmd) ([]string, []error) {
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
	return outputs, errs
}

// runConcurrently runs cmds at the same time and returns their combined
// output. It fails the test if one of them fails.
func runConcurrently(t *testing.T, cmds []*exec.Cmd) []string {
	t.Helper()
	outputs, errs := startConcurrently(cmds)
	for i, cmd := range cmds {
		if errs[i] != nil {
			t.Errorf("%s in %s failed: %v\n%s", cmd.String(), cmd.Dir, errs[i], outputs[i])
		}
	}
	if t.Failed() {
		t.FailNow()
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
