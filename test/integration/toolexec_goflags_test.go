// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

func preparedGoMod(repoRoot string) string {
	return fmt.Sprintf(`module example.com/otelc-prepared

go 1.25.0

require (
	example.com/otelc-prepared/hooks v0.0.0
	go.opentelemetry.io/otelc/pkg v0.0.0
)

replace (
	example.com/otelc-prepared/hooks => ./hooks
	go.opentelemetry.io/otelc => %s
	go.opentelemetry.io/otelc/pkg => %s/pkg
)
`, repoRoot, repoRoot)
}

const preparedToolsFile = `//go:build tools

package tools

import _ "example.com/otelc-prepared/hooks"
`

func preparedHooksGoMod(repoRoot string) string {
	return fmt.Sprintf(`module example.com/otelc-prepared/hooks

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s/pkg
`, repoRoot)
}

const preparedHooksSource = `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"fmt"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func Before(ctx hook.HookContext) {
	_ = ctx
	fmt.Println("prepared hook executed")
}
`

const preparedHooksYaml = `hook_work:
  target:
    - main
    - $root
  where:
    func: Work
  do:
    - inject_hooks:
        before: Before
        path: example.com/otelc-prepared/hooks
`

const preparedMainSource = `package main

import "fmt"

func Work() {
	fmt.Println("prepared work executed")
}

func main() {
	Work()
}
`

func TestGOFLAGSPreparedBuild(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	absoluteOtelcPath, err := filepath.Abs(otelcPath)
	require.NoError(t, err)
	require.FileExists(t, absoluteOtelcPath)
	baseEnv := preparedBuildBaseEnvironment()
	goFlags := strconv.Quote("-toolexec=" + absoluteOtelcPath + " toolexec")

	t.Run("fresh cache with empty work dir env", func(t *testing.T) {
		build := newPreparedBuildCase(t, absoluteOtelcPath, baseEnv, goFlags, ".")
		build.setup()

		_, err := os.Stat(build.goCache)
		require.ErrorIs(t, err, os.ErrNotExist, "GOCACHE must not exist before go build")
		build.directBuild(build.moduleDir, true)
		require.DirExists(t, build.goCache, "go build did not create the fresh GOCACHE")
		build.runAndRequireExecution()
	})

	t.Run("warm cache", func(t *testing.T) {
		build := newPreparedBuildCase(t, absoluteOtelcPath, baseEnv, goFlags, ".")

		_, err := os.Stat(build.goCache)
		require.ErrorIs(t, err, os.ErrNotExist, "GOCACHE must not exist before plain go build")
		build.plainBuild()
		require.DirExists(t, build.goCache, "plain go build did not create the fresh GOCACHE")

		build.setup()
		build.directBuild(build.moduleDir, false)
		require.DirExists(t, build.goCache, "direct go build removed the warm GOCACHE")
		build.runAndRequireExecution()
	})

	t.Run("nested module directory", func(t *testing.T) {
		build := newPreparedBuildCase(t, absoluteOtelcPath, baseEnv, goFlags, "cmd/app")
		build.setup("./cmd/app")
		build.directBuild(filepath.Join(build.moduleDir, "cmd", "app"), false)
		build.runAndRequireExecution()
	})

	t.Run("re-prepared cache", func(t *testing.T) {
		build := newPreparedBuildCase(t, absoluteOtelcPath, baseEnv, goFlags, ".")
		build.setup()
		build.directBuild(build.moduleDir, false)
		build.runAndRequireExecution()
		require.DirExists(t, build.goCache, "first direct build did not create GOCACHE")

		build.cleanup()
		require.DirExists(t, build.goCache, "otelc cleanup removed the shared GOCACHE")
		build.setup()

		build.directBuild(build.moduleDir, false)
		build.runAndRequireExecution()
	})

	t.Run("debug diff direct mode integration", func(t *testing.T) {
		debugEnv := append(baseEnv, "OTELC_DEBUG=1")
		build := newPreparedBuildCase(t, absoluteOtelcPath, debugEnv, goFlags, ".")

		// 1. Plant a stale debug artifact from a previous run
		staleDir := filepath.Join(build.moduleDir, ".otelc-build", "debug", "stale_pkg")
		require.NoError(t, os.MkdirAll(staleDir, 0o755))
		staleDiff := filepath.Join(staleDir, "stale.go.diff")
		require.NoError(t, os.WriteFile(staleDiff, []byte("stale diff content"), 0o644))

		// 2. Run otelc setup under debug to establish the diagnostic lifecycle boundary
		build.setup()

		// Verify stale debug files were cleared by setup
		require.NoFileExists(t, staleDiff, "otelc setup must clear stale debug artifacts under debug mode")

		debugDir := filepath.Join(build.moduleDir, ".otelc-build", "debug")
		require.DirExists(t, debugDir)

		// Verify setup runtime diff was generated in the package directory
		runtimeDiff := filepath.Join(debugDir, "example_com_otelc-prepared", "otelc.runtime.go.diff")
		require.FileExists(t, runtimeDiff)

		// 3. Run direct OTELC_DEBUG=1 go build -a to force package recompilation
		build.directBuild(build.moduleDir, false, "-a")

		compilerDiff := filepath.Join(debugDir, "main", "main.go.diff")
		require.FileExists(t, compilerDiff)

		// 5. Verify runtime diff remains available
		require.FileExists(t, runtimeDiff)

		// Verify binary runs and executes hook
		build.runAndRequireExecution()
	})
}

type preparedBuildCase struct {
	t         *testing.T
	otelcPath string
	baseEnv   []string
	goFlags   string
	workspace string
	moduleDir string
	goCache   string
	prepared  bool
}

func newPreparedBuildCase(
	t *testing.T,
	otelcPath string,
	baseEnv []string,
	goFlags string,
	mainPackage string,
) *preparedBuildCase {
	t.Helper()

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	repoRootSlash := filepath.ToSlash(repoRoot)

	workspace := t.TempDir()
	moduleDir := filepath.Join(workspace, "module")
	mainDir := filepath.Join(moduleDir, filepath.FromSlash(mainPackage))
	hooksDir := filepath.Join(moduleDir, "hooks")
	require.NoError(t, os.MkdirAll(mainDir, 0o755))
	require.NoError(t, os.MkdirAll(hooksDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(preparedGoMod(repoRootSlash)), 0o644))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(moduleDir, "otel.instrumentation.go"), []byte(preparedToolsFile), 0o644),
	)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(hooksDir, "go.mod"), []byte(preparedHooksGoMod(repoRootSlash)), 0o644),
	)
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "hooks.go"), []byte(preparedHooksSource), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "otelc.yaml"), []byte(preparedHooksYaml), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(mainDir, "main.go"), []byte(preparedMainSource), 0o644))

	build := &preparedBuildCase{
		t:         t,
		otelcPath: otelcPath,
		baseEnv:   baseEnv,
		goFlags:   goFlags,
		workspace: workspace,
		moduleDir: moduleDir,
		goCache:   filepath.Join(workspace, "gocache"),
	}
	t.Cleanup(func() {
		if !build.prepared {
			return
		}
		output, err := build.cleanupCommand()
		if err != nil {
			t.Errorf("otelc cleanup failed: %v\n%s", err, output)
			return
		}
		build.prepared = false
	})
	return build
}

func (b *preparedBuildCase) setup(args ...string) {
	b.t.Helper()

	cmd := exec.CommandContext(b.t.Context(), b.otelcPath, append([]string{"setup"}, args...)...)
	cmd.Dir = b.moduleDir
	cmd.Env = b.baseEnv
	output, err := cmd.CombinedOutput()
	require.NoError(b.t, err, "otelc setup failed:\n%s", output)
	b.prepared = true

	matchedPath := filepath.Join(b.moduleDir, ".otelc-build", "matched.json")
	matched, err := os.ReadFile(matchedPath)
	require.NoError(b.t, err)
	require.Contains(b.t, string(matched), "example.com/otelc-prepared/hooks")
}

func (b *preparedBuildCase) cleanup() {
	b.t.Helper()

	output, err := b.cleanupCommand()
	require.NoError(b.t, err, "otelc cleanup failed:\n%s", output)
	b.prepared = false
}

func (b *preparedBuildCase) cleanupCommand() ([]byte, error) {
	cmd := exec.Command(b.otelcPath, "cleanup")
	cmd.Dir = b.moduleDir
	cmd.Env = b.baseEnv
	return cmd.CombinedOutput()
}

func (b *preparedBuildCase) appPath() string {
	name := "app"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(b.moduleDir, name)
}

func (b *preparedBuildCase) plainBuild() {
	b.t.Helper()

	cmd := exec.CommandContext(b.t.Context(), "go", "build", "-o", b.appPath(), ".")
	cmd.Dir = b.moduleDir
	cmd.Env = preparedPlainBuildEnvironment(b.baseEnv, b.goCache)
	output, err := cmd.CombinedOutput()
	require.NoError(b.t, err, "plain go build failed:\n%s", output)
}

func (b *preparedBuildCase) directBuild(buildDir string, emptyWorkDirEnv bool, extraArgs ...string) {
	b.t.Helper()

	args := append([]string{"build", "-o", b.appPath()}, extraArgs...)
	args = append(args, ".")
	cmd := exec.CommandContext(b.t.Context(), "go", args...)
	cmd.Dir = buildDir
	cmd.Env = preparedDropInBuildEnvironment(b.baseEnv, b.goCache, b.goFlags)
	if emptyWorkDirEnv {
		cmd.Env = append(cmd.Env, "OTELC_WORK_DIR=")
	}
	output, err := cmd.CombinedOutput()
	require.NoError(b.t, err, "go build failed:\n%s", output)
}

func (b *preparedBuildCase) runAndRequireExecution() {
	b.t.Helper()

	cmd := exec.CommandContext(b.t.Context(), b.appPath())
	cmd.Dir = b.moduleDir
	output, err := cmd.CombinedOutput()
	require.NoError(b.t, err, "running app failed:\n%s", output)
	require.Contains(b.t, string(output), "prepared hook executed")
	require.Contains(b.t, string(output), "prepared work executed")
}

func preparedBuildBaseEnvironment() []string {
	env := os.Environ()
	filtered := env[:0]
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		name = strings.ToLower(name)
		if name == "goenv" || name == "goflags" || name == "gocache" || name == "gowork" ||
			strings.HasPrefix(name, "otelc_") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, "GOENV=off")
}

func preparedPlainBuildEnvironment(baseEnv []string, goCache string) []string {
	env := make([]string, len(baseEnv), len(baseEnv)+2)
	copy(env, baseEnv)
	return append(env, "GOWORK=off", "GOCACHE="+goCache)
}

func preparedDropInBuildEnvironment(baseEnv []string, goCache, goFlags string) []string {
	env := make([]string, len(baseEnv), len(baseEnv)+3)
	copy(env, baseEnv)
	return append(env, "GOWORK=off", "GOCACHE="+goCache, "GOFLAGS="+goFlags)
}
