// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"net/http"
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

const preparedGoMod = `module example.com/otelc-prepared

go 1.25.0
`

const preparedMainSource = `package main

import (
	"flag"
	"io"
	"net/http"
)

func main() {
	addr := flag.String("addr", "", "server address")
	flag.Parse()

	resp, err := http.Get(*addr + "/hello")
	if err != nil {
		panic(err)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		_ = resp.Body.Close()
		panic(err)
	}
	if err := resp.Body.Close(); err != nil {
		panic(err)
	}
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
		build.runAndRequireHTTPSpan()
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
		build.runAndRequireHTTPSpan()
	})

	t.Run("nested module directory", func(t *testing.T) {
		build := newPreparedBuildCase(t, absoluteOtelcPath, baseEnv, goFlags, "cmd/app")
		build.setup("./cmd/app")
		build.directBuild(filepath.Join(build.moduleDir, "cmd", "app"), false)
		build.runAndRequireHTTPSpan()
	})

	t.Run("re-prepared cache", func(t *testing.T) {
		build := newPreparedBuildCase(t, absoluteOtelcPath, baseEnv, goFlags, ".")
		build.setup()
		build.directBuild(build.moduleDir, false)
		build.runAndRequireHTTPSpan()
		require.DirExists(t, build.goCache, "first direct build did not create GOCACHE")

		build.cleanup()
		require.DirExists(t, build.goCache, "otelc cleanup removed the shared GOCACHE")
		build.setup()

		build.directBuild(build.moduleDir, false)
		build.runAndRequireHTTPSpan()
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

	workspace := t.TempDir()
	moduleDir := filepath.Join(workspace, "module")
	mainDir := filepath.Join(moduleDir, filepath.FromSlash(mainPackage))
	require.NoError(t, os.MkdirAll(mainDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(preparedGoMod), 0o644))
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
	require.Contains(b.t, string(matched), "/instrumentation/net/http/client")
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

func (b *preparedBuildCase) directBuild(buildDir string, emptyWorkDirEnv bool) {
	b.t.Helper()

	cmd := exec.CommandContext(b.t.Context(), "go", "build", "-o", b.appPath(), ".")
	cmd.Dir = buildDir
	cmd.Env = preparedDropInBuildEnvironment(b.baseEnv, b.goCache, b.goFlags)
	if emptyWorkDirEnv {
		cmd.Env = append(cmd.Env, "OTELC_WORK_DIR=")
	}
	output, err := cmd.CombinedOutput()
	require.NoError(b.t, err, "go build failed:\n%s", output)
}

func (b *preparedBuildCase) runAndRequireHTTPSpan() {
	b.t.Helper()

	server := StartHTTPServerWithResponse(b.t, http.StatusOK, `{"message":"Hello"}`)
	fixture := testutil.NewTestFixture(b.t, testutil.WithAppsDir(b.workspace))
	fixture.Run("module", "-addr="+server.URL)

	span := fixture.RequireSingleSpan()
	testutil.RequireHTTPClientSemconv(
		b.t,
		span,
		http.MethodGet,
		server.URL+"/hello",
		"127.0.0.1",
		http.StatusOK,
		server.Port(),
		"1.1",
		"http",
	)
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
