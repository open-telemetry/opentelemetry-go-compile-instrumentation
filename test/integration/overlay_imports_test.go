// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"

	"go.opentelemetry.io/otelc/test/testutil"
	"go.opentelemetry.io/otelc/tool/util"
)

// overlayDep is a module served from a local GOPROXY, so the go command
// extracts it into the read-only module cache like any downloaded dependency.
// otelcovdep does not import container/list; the rules below add that import.
const (
	overlayDepPath    = "example.com/otelcovdep"
	overlayDepVersion = "v1.0.0"
)

// writeFileProxy serves overlayDep from a GOPROXY directory and returns its
// file:// URL.
func writeFileProxy(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	writeTestFile(t, src, "go.mod", "module "+overlayDepPath+"\n\ngo 1.25.0\n")
	writeTestFile(t, src, "dep.go", `package otelcovdep

// Hello reports what the dependency returns.
func Hello() string {
	return "plain"
}
`)
	root := t.TempDir()
	escaped, err := module.EscapePath(overlayDepPath)
	require.NoError(t, err)
	vdir := filepath.Join(root, filepath.FromSlash(escaped), "@v")
	require.NoError(t, os.MkdirAll(vdir, 0o755))
	writeTestFile(t, vdir, "list", overlayDepVersion+"\n")
	writeTestFile(t, vdir, overlayDepVersion+".info", `{"Version":"`+overlayDepVersion+`"}`)
	writeTestFile(t, vdir, overlayDepVersion+".mod", "module "+overlayDepPath+"\n\ngo 1.25.0\n")
	zf, err := os.Create(filepath.Join(vdir, overlayDepVersion+".zip"))
	require.NoError(t, err)
	require.NoError(t, modzip.CreateFromDir(zf, module.Version{Path: overlayDepPath, Version: overlayDepVersion}, src))
	require.NoError(t, zf.Close())
	return "file:///" + strings.TrimPrefix(filepath.ToSlash(root), "/")
}

type overlayApp struct {
	dir, otelc, binary string
	env                []string
}

// newOverlayApp writes an app that requires overlayDep and prints what it
// and path.Clean return, with rules as its OTELC_RULES file.
func newOverlayApp(t *testing.T, rules string) *overlayApp {
	t.Helper()
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	app := &overlayApp{dir: t.TempDir(), otelc: otelcPath}
	app.binary = filepath.Join(app.dir, "app")
	if util.IsWindows() {
		app.binary += ".exe"
	}
	writeTestFile(t, app.dir, "go.mod", fmt.Sprintf(`module example.com/ovapp

go 1.25.0

require (
	%s %s
	go.opentelemetry.io/otelc/pkg v0.0.0
)

replace go.opentelemetry.io/otelc/pkg => %s
`, overlayDepPath, overlayDepVersion, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))
	writeTestFile(t, app.dir, "main.go", `package main

import (
	"fmt"
	"path"

	"example.com/otelcovdep"
	"example.com/ovapp/lib"
)

func main() {
	fmt.Println("dep:", otelcovdep.Hello())
	fmt.Println("std:", path.Clean("otelc-probe"))
	fmt.Println("lib:", lib.Name())
}
`)
	writeTestFile(t, app.dir, filepath.Join("lib", "lib.go"), `package lib

// Name reports the library name.
func Name() string {
	return "lib"
}
`)
	writeTestFile(t, app.dir, "rules.yml", rules)

	app.env = append(preparedBuildBaseEnvironment(),
		"GOWORK=off",
		"GOCACHE="+filepath.Join(t.TempDir(), "gocache"),
		"GOPROXY="+writeFileProxy(t)+",https://proxy.golang.org,direct",
		"GONOSUMDB=example.com",
		"OTELC_RULES=rules.yml",
		"OTELC_STATS=1",
	)
	tidy := exec.CommandContext(t.Context(), "go", "mod", "tidy")
	tidy.Dir = app.dir
	tidy.Env = app.env
	out, err := tidy.CombinedOutput()
	require.NoError(t, err, "go mod tidy failed:\n%s", out)
	return app
}

func (a *overlayApp) build(t *testing.T, args ...string) string {
	t.Helper()
	return runOtelcCommand(t, a.dir, a.env, a.otelc, append([]string{"go", "build", "-o", a.binary}, args...)...)
}

func (a *overlayApp) run(t *testing.T) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), a.binary)
	cmd.Dir = a.dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "running the instrumented binary failed:\n%s", out)
	return string(out)
}

// listRule returns a rule that makes fn in target return a value built with
// container/list, which target does not import.
func listRule(name, target, fn, value string) string {
	return fmt.Sprintf(`%s:
  target: %s
  where:
    func: %s
  do:
    - inject_code:
        raw: |
          l := list.New()
          l.PushBack(%q)
          return l.Front().Value.(string)
  imports:
    list: container/list
`, name, target, fn, value)
}

func TestOverlayImportIntoModCachePackage(t *testing.T) {
	app := newOverlayApp(t, listRule("dep_list", overlayDepPath, "Hello", "dep-instrumented"))

	app.build(t, ".")
	assert.Contains(t, app.run(t), "dep: dep-instrumented")
	assertStatsCompiledPackage(t, statsLog(t, app.dir), overlayDepPath)

	version := exec.CommandContext(t.Context(), "go", "version", "-m", app.binary)
	version.Env = app.env
	out, err := version.CombinedOutput()
	require.NoError(t, err, "%s", out)
	assert.Contains(t, string(out), "=>", "the module whose package gains an import is replaced")

	t.Run("warm build reuses the cache", func(t *testing.T) {
		require.NoError(t, os.RemoveAll(filepath.Join(app.dir, ".otelc-build")))
		app.build(t, ".")
		assert.Contains(t, app.run(t), "dep: dep-instrumented")
		assertStatsCacheHitPackage(t, statsLog(t, app.dir), overlayDepPath)
	})
}

func TestOverlayImportIntoStdPackage(t *testing.T) {
	app := newOverlayApp(t, `path_list:
  target: path
  where:
    func: Clean
  do:
    - inject_code:
        raw: |
          if path == "otelc-probe" {
            l := list.New()
            l.PushBack("std-instrumented")
            return l.Front().Value.(string)
          }
  imports:
    list: container/list
`)

	app.build(t, ".")
	assert.Contains(t, app.run(t), "std: std-instrumented")
}

func TestOverlayImportWithUserOverlay(t *testing.T) {
	app := newOverlayApp(t, listRule("lib_list", "example.com/ovapp/lib", "Name", "lib-instrumented"))
	replacement := filepath.Join(t.TempDir(), "dep.go")
	writeTestFile(t, filepath.Dir(replacement), "dep.go", `package main

func init() { println("user overlay applied") }
`)
	userOverlay := filepath.Join(t.TempDir(), "user.json")
	writeTestFile(t, filepath.Dir(userOverlay), "user.json",
		fmt.Sprintf(`{"Replace":{%q:%q}}`, filepath.Join(app.dir, "user_overlay.go"), replacement))

	app.build(t, "-overlay", userOverlay, ".")
	out := app.run(t)
	assert.Contains(t, out, "user overlay applied")
	assert.Contains(t, out, "lib: lib-instrumented")
}

func TestOverlayImportWorkspace(t *testing.T) {
	app := newOverlayApp(t, listRule("dep_list", overlayDepPath, "Hello", "dep-instrumented"))
	writeTestFile(t, app.dir, "go.work", "go 1.25.0\n\nuse .\n")
	for i, entry := range app.env {
		if entry == "GOWORK=off" {
			app.env[i] = "GOWORK=" + filepath.Join(app.dir, "go.work")
		}
	}

	app.build(t, ".")
	assert.Contains(t, app.run(t), "dep: dep-instrumented")
}

func TestOverlayImportDropIn(t *testing.T) {
	app := newOverlayApp(t, listRule("lib_list", "example.com/ovapp/lib", "Name", "lib-instrumented"))

	setupOut := runOtelcCommand(t, app.dir, app.env, app.otelc, "setup")
	overlay := filepath.Join(app.dir, ".otelc-build", "overlay.json")
	require.Contains(t, setupOut, "-overlay="+overlay)

	toolexec, err := util.QuoteGoflagsToken("-toolexec=" + app.otelc + " toolexec")
	require.NoError(t, err)
	goBuild := func(goflags string) ([]byte, error) {
		cmd := exec.CommandContext(t.Context(), "go", "build", "-o", app.binary, ".")
		cmd.Dir = app.dir
		cmd.Env = append(app.env, "GOFLAGS="+goflags)
		return cmd.CombinedOutput()
	}

	out, err := goBuild(toolexec)
	require.Error(t, err, "a drop-in build without the overlay cannot compile the added import:\n%s", out)
	assert.Contains(t, string(out), "-overlay")

	out, err = goBuild(toolexec + " -overlay=" + overlay)
	require.NoError(t, err, "%s", out)
	assert.Contains(t, app.run(t), "lib: lib-instrumented")
}

func TestOverlayImportCycleReported(t *testing.T) {
	app := newOverlayApp(t, `lib_imports_back:
  target: example.com/ovapp/lib
  where:
    func: Name
  do:
    - inject_code:
        raw: |
          _ = back.Name
  imports:
    back: example.com/ovapp/back
`)
	writeTestFile(t, app.dir, filepath.Join("back", "back.go"), `package back

import "example.com/ovapp/lib"

// Name forwards to lib.
func Name() string { return lib.Name() }
`)

	cmd := exec.CommandContext(t.Context(), app.otelc, "go", "build", "-o", app.binary, ".")
	cmd.Dir = app.dir
	cmd.Env = app.env
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(out), "import cycle not allowed")
}

// TestOverlayGoTestWithBundledRules runs otelc go test with the bundled rules.
// Under go test, target main matches the testmain the go command generates,
// whose imports no overlay can change, so the bundled init_sdk file must not
// add any.
func TestOverlayGoTestWithBundledRules(t *testing.T) {
	app := newOverlayApp(t, "")
	writeTestFile(t, app.dir, "main_test.go", `package main

import "testing"

func TestMain_(t *testing.T) { main() }
`)
	env := app.env[:0:0]
	for _, entry := range app.env {
		if !strings.HasPrefix(entry, "OTELC_RULES=") {
			env = append(env, entry)
		}
	}

	out := runOtelcCommand(t, app.dir, env, app.otelc, "go", "test", "-count=1", "-v", ".")
	assert.Contains(t, out, "PASS")
}
