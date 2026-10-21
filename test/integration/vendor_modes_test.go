//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

// The tests in this file define the contract of otelc for projects that vendor
// their dependencies:
//   - otelc keeps vendor mode. The dependencies of the user come only from
//     vendor/, and otelc never downloads them.
//   - otelc adds the modules of the hook code to the build for the time of the
//     build only. After the build, go.mod, go.sum and vendor/ are the same as
//     before.
//   - `otelc vendor` adds the modules of the hook code to vendor/ for all
//     time, so that a later build needs no network and no module cache.
//   - When the hook code needs a higher version of a module than the vendored
//     one, the build fails with a clear message. otelc never changes vendored
//     code.

const vendorModesApp = "vendmodes"

const vendorModesGoMod = `module vendmodes

go 1.25.0

require github.com/gin-gonic/gin v1.12.0
`

// vendorModesMainTemplate is a gin server with one route. IMPORTS and BODY are
// replaced: IMPORTS adds import lines, BODY is the arguments of c.String.
const vendorModesMainTemplate = `package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
	IMPORTS
)

func main() {
	port := flag.Int("port", 8080, "listen port")
	flag.Parse()

	r := gin.New()
	r.GET("/hello", func(c *gin.Context) {
		c.String(200, BODY)
	})

	if err := r.Run(fmt.Sprintf(":%d", *port)); err != nil {
		log.Fatal(err)
	}
}
`

const (
	privateModule     = "corp.example/private"
	privateGreeting   = "hello from corp.example/private"
	patchedGinVersion = "PATCHED"
	oldOtelVersion    = "v1.30.0"
	otelModule        = "go.opentelemetry.io/otel"
)

func vendorModesMain(imports, body string) string {
	return strings.NewReplacer("IMPORTS", imports, "BODY", body).Replace(vendorModesMainTemplate)
}

// vendorFixture is a test app: appsDir is the apps directory for testutil, and
// dir is the module directory of the app in it.
type vendorFixture struct {
	appsDir string
	dir     string
}

// newVendoredGinApp writes a gin app in a new apps directory, and runs go mod
// tidy and go mod vendor. The network is used here, with the module cache of
// the environment.
func newVendoredGinApp(t *testing.T, goMod, mainGo string) vendorFixture {
	t.Helper()
	appsDir := t.TempDir()
	appDir := filepath.Join(appsDir, vendorModesApp)
	writeTestFile(t, appDir, "go.mod", goMod)
	writeTestFile(t, appDir, "main.go", mainGo)

	env := vendorTestEnv()
	runGo(t, appDir, env, "mod", "tidy")
	goGetHookDeps(t, appDir, env)
	runGo(t, appDir, env, "mod", "vendor")
	return vendorFixture{appsDir: appsDir, dir: appDir}
}

// vendorTestEnv returns the environment of the test process, with the given
// overrides. GOFLAGS is cleared and the workspace and toolchain switching are
// disabled, so that ambient settings cannot change the result. exec.Cmd uses
// the last value of a duplicated variable, so the overrides win.
func vendorTestEnv(overrides ...string) []string {
	env := append([]string{}, os.Environ()...)
	env = append(env, "GOFLAGS=", "GOWORK=off", "GOTOOLCHAIN=local")
	return append(env, overrides...)
}

// offlineEnv returns overrides that disable all module downloads.
func offlineEnv(goModCache string) []string {
	return []string{"GOPROXY=off", "GOSUMDB=off", "GOMODCACHE=" + goModCache}
}

// newModCache returns a new empty module cache directory. The go command
// writes the module cache as read-only, so it is removed with go clean
// -modcache before t.TempDir removes the directory.
func newModCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() {
		// t.Context is canceled when cleanup functions run.
		cmd := exec.CommandContext(context.Background(), "go", "clean", "-modcache")
		cmd.Env = vendorTestEnv("GOMODCACHE=" + dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("go clean -modcache failed: %v\n%s", err, out)
		}
	})
	return dir
}

func runGo(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go %s failed:\n%s", strings.Join(args, " "), out)
}

// vendorModesBin returns the name of the binary of the test app. On
// Windows it has the .exe extension, as testutil.Start expects.
func vendorModesBin() string {
	if runtime.GOOS == "windows" {
		return "app.exe"
	}
	return "app"
}

// otelcGoBuild runs `otelc go build -o app .` in appDir with env. It returns
// the combined output and the error, so that a test can examine a failed
// build.
func otelcGoBuild(t *testing.T, appDir string, env []string) (string, error) {
	t.Helper()
	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelc, err = filepath.Abs(otelc)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(appDir, vendorModesBin()))
		_ = os.RemoveAll(filepath.Join(appDir, ".otelc-build"))
		_ = os.Remove(filepath.Join(appDir, ".otelc-build.lock"))
	})

	cmd := exec.CommandContext(t.Context(), otelc, "go", "build", "-o", vendorModesBin(), ".")
	cmd.Dir = appDir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runOtelcVendor runs `otelc vendor` in appDir with env and requires success.
func runOtelcVendor(t *testing.T, appDir string, env []string) {
	t.Helper()
	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelc, err = filepath.Abs(otelc)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(appDir, ".otelc-build"))
		_ = os.Remove(filepath.Join(appDir, ".otelc-build.lock"))
	})
	runOtelcCommand(t, appDir, env, otelc, "vendor")
}

// moduleFiles hashes go.mod, go.sum and all the files in vendor/. The result
// maps each relative path (slash separated) to the SHA-256 of its content.
func moduleFiles(t *testing.T, appDir string) map[string]string {
	t.Helper()
	return hashTree(t, appDir, func(rel string) bool {
		return rel == "go.mod" || rel == "go.sum" || strings.HasPrefix(rel, "vendor/")
	})
}

// sourceTree hashes all the files in appDir, but not the build outputs and
// the otelc work files.
func sourceTree(t *testing.T, appDir string) map[string]string {
	t.Helper()
	return hashTree(t, appDir, func(rel string) bool {
		switch {
		case rel == vendorModesBin(), rel == "plain.bin", rel == ".otelc-build.lock":
			return false
		case rel == ".otelc-build" || strings.HasPrefix(rel, ".otelc-build/"):
			return false
		}
		return true
	})
}

func hashTree(t *testing.T, root string, include func(rel string) bool) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == ".otelc-build" {
				// The otelc work directory changes on each build.
				return filepath.SkipDir
			}
			return nil
		}
		if !include(rel) {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		if _, copyErr := io.Copy(h, f); copyErr != nil {
			return copyErr
		}
		out[rel] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	require.NoError(t, err)
	return out
}

// requireNoModModLeak checks that otelc did not switch the build to module
// mode: the debug log of otelc must not show -mod=mod.
func requireNoModModLeak(t *testing.T, appDir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(appDir, ".otelc-build", "debug.log"))
	if os.IsNotExist(err) {
		return
	}
	require.NoError(t, err)
	// Report only the lines with -mod=mod: the full log is very long.
	var leaks []string
	for line := range strings.Lines(string(data)) {
		if strings.Contains(line, "-mod=mod") {
			leaks = append(leaks, truncate(line, 300))
		}
	}
	assert.Empty(t, leaks, "otelc must keep vendor mode, not force -mod=mod")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// requireServerSpan starts the built app, sends one request and checks that
// the response contains want and that a server span was exported.
func requireServerSpan(t *testing.T, appsDir, want string) {
	t.Helper()
	port := testutil.FreePort(t)
	f := testutil.NewTestFixture(t, testutil.WithAppsDir(appsDir))
	f.Start(vendorModesApp, fmt.Sprintf("-port=%d", port))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d/hello", port), nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(body), want)

	f.WaitForSpans(1)
	span := testutil.RequireSpan(t, f.Traces(), testutil.IsServer)
	require.Equal(t, "GET /hello", span.Name())
}

// addPrivateVendoredModule adds corp.example/private v1.0.0 to the app. The
// module is only in vendor/: no proxy or VCS server has it, so the build
// fails if the go command tries to download it.
func addPrivateVendoredModule(t *testing.T, appDir string) {
	t.Helper()
	goMod, err := os.ReadFile(filepath.Join(appDir, "go.mod"))
	require.NoError(t, err)
	goMod = append(goMod, []byte("\nrequire "+privateModule+" v1.0.0\n")...)
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "go.mod"), goMod, 0o644))

	writeTestFile(t, appDir, "vendor/corp.example/private/private.go",
		"package private\n\nfunc Hello() string { return \""+privateGreeting+"\" }\n")

	modulesTxt := filepath.Join(appDir, "vendor", "modules.txt")
	data, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	data = append(data, []byte("# "+privateModule+" v1.0.0\n## explicit\n"+privateModule+"\n")...)
	require.NoError(t, os.WriteFile(modulesTxt, data, 0o644))
}

// requirePlainVendoredBuild checks that the fixture is valid: the go command
// alone builds the app from vendor/ with no network and no module cache.
func requirePlainVendoredBuild(t *testing.T, appDir string) {
	t.Helper()
	env := vendorTestEnv(append(offlineEnv(newModCache(t)), "GOFLAGS=-mod=vendor")...)
	runGo(t, appDir, env, "build", "-o", "plain.bin", ".")
	require.NoError(t, os.Remove(filepath.Join(appDir, "plain.bin")))
}

// TestVendoredPrivateModule: the app uses a module that exists only in
// vendor/. otelc must take it from vendor/ and never try to download it.
func TestVendoredPrivateModule(t *testing.T) {
	t.Parallel()

	app := newVendoredGinApp(t, vendorModesGoMod, vendorModesMain("", `"ok"`))
	appsDir, appDir := app.appsDir, app.dir
	addPrivateVendoredModule(t, appDir)
	writeTestFile(t, appDir, "main.go",
		vendorModesMain(`"`+privateModule+`"`, `"%s", private.Hello()`))
	requirePlainVendoredBuild(t, appDir)

	before := moduleFiles(t, appDir)
	out, err := otelcGoBuild(t, appDir, vendorTestEnv())
	assert.Equal(t, before, moduleFiles(t, appDir), "go.mod, go.sum and vendor/ must not change")
	require.NoError(t, err, "otelc go build failed:\n%s", out)
	requireNoModModLeak(t, appDir)

	requireServerSpan(t, appsDir, privateGreeting)
}

// TestVendoredPatchIsKept: a change to a vendored file must be in the
// instrumented binary. otelc must not take the code from the module cache.
func TestVendoredPatchIsKept(t *testing.T) {
	t.Parallel()

	app := newVendoredGinApp(t, vendorModesGoMod, vendorModesMain("", `"%s", gin.Version`))
	appsDir, appDir := app.appsDir, app.dir

	versionFile := filepath.Join(appDir, "vendor", "github.com", "gin-gonic", "gin", "version.go")
	data, err := os.ReadFile(versionFile)
	require.NoError(t, err)
	patched := strings.Replace(string(data), `const Version = "v1.12.0"`,
		`const Version = "`+patchedGinVersion+`"`, 1)
	require.NotEqual(t, string(data), patched, "the patch must change version.go")
	require.NoError(t, os.WriteFile(versionFile, []byte(patched), 0o644))

	before := moduleFiles(t, appDir)
	out, err := otelcGoBuild(t, appDir, vendorTestEnv())
	assert.Equal(t, before, moduleFiles(t, appDir), "go.mod, go.sum and vendor/ must not change")
	require.NoError(t, err, "otelc go build failed:\n%s", out)
	requireNoModModLeak(t, appDir)

	requireServerSpan(t, appsDir, patchedGinVersion)
}

// TestVendoredOfflineWithPrimedCache: the first build (with network) puts
// only the modules of the hook code in the module cache, never the modules of
// the user. A second build with no network then uses that cache.
func TestVendoredOfflineWithPrimedCache(t *testing.T) {
	t.Parallel()

	app := newVendoredGinApp(t, vendorModesGoMod, vendorModesMain("", `"ok"`))
	appsDir, appDir := app.appsDir, app.dir
	modCache := newModCache(t)

	out, err := otelcGoBuild(t, appDir, vendorTestEnv("GOMODCACHE="+modCache))
	require.NoError(t, err, "otelc go build with network failed:\n%s", out)

	var userModulePaths []string
	require.NoError(t, filepath.WalkDir(modCache, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(filepath.ToSlash(path), "github.com/gin-gonic") {
			rel, relErr := filepath.Rel(modCache, path)
			if relErr != nil {
				return relErr
			}
			userModulePaths = append(userModulePaths, filepath.ToSlash(rel))
			if len(userModulePaths) == 5 {
				// Some examples are sufficient for the failure message.
				return filepath.SkipAll
			}
		}
		return nil
	}))
	assert.Empty(t, userModulePaths, "otelc must not download vendored user modules")

	// Remove the otelc work directory, so that the second build must resolve
	// the hook modules again, from the module cache only.
	require.NoError(t, os.RemoveAll(filepath.Join(appDir, ".otelc-build")))
	require.NoError(t, os.Remove(filepath.Join(appDir, vendorModesBin())))

	out, err = otelcGoBuild(t, appDir, vendorTestEnv(offlineEnv(modCache)...))
	require.NoError(t, err, "otelc go build without network failed:\n%s", out)
	requireNoModModLeak(t, appDir)

	requireServerSpan(t, appsDir, "ok")
}

// TestVendoredAirGappedAfterOtelcVendor: after `otelc vendor`, the project
// builds with no network and an empty module cache, with and without otelc.
func TestVendoredAirGappedAfterOtelcVendor(t *testing.T) {
	t.Parallel()

	app := newVendoredGinApp(t, vendorModesGoMod, vendorModesMain("", `"ok"`))
	appsDir, appDir := app.appsDir, app.dir

	runOtelcVendor(t, appDir, vendorTestEnv())
	modulesTxt, err := os.ReadFile(filepath.Join(appDir, "vendor", "modules.txt"))
	require.NoError(t, err)
	require.Regexp(t, `(?m)^# `+regexp.QuoteMeta(otelModule)+` v\S+`, string(modulesTxt),
		"otelc vendor must add the otel module to vendor/modules.txt")
	require.DirExists(t, filepath.Join(appDir, "vendor", "go.opentelemetry.io", "otel"))

	snapshot := sourceTree(t, appDir)

	// The result of otelc vendor is a valid vendor directory for the go
	// command alone: committing it does not break the normal build.
	requirePlainVendoredBuild(t, appDir)

	env := vendorTestEnv(append(offlineEnv(newModCache(t)), "GOFLAGS=-mod=vendor")...)
	out, err := otelcGoBuild(t, appDir, env)
	assert.Equal(t, snapshot, sourceTree(t, appDir), "the build must not change the project files")
	require.NoError(t, err, "air-gapped otelc go build failed:\n%s", out)
	requireNoModModLeak(t, appDir)

	requireServerSpan(t, appsDir, "ok")
}

// requiredOtelVersion returns the version of go.opentelemetry.io/otel that
// the otelc runtime package requires.
func requiredOtelVersion(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "pkg", "runtime", "go.mod"))
	require.NoError(t, err)
	m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(otelModule) + `\s+(v\S+)`).FindSubmatch(data)
	require.NotNil(t, m, "pkg/runtime/go.mod must require %s", otelModule)
	return string(m[1])
}

// TestVendoredVersionConflict: the user vendors an old otel version, and the
// hook code needs a higher one. otelc must not change the vendored code: the
// build fails and tells the module, the vendored and the required versions.
func TestVendoredVersionConflict(t *testing.T) {
	t.Parallel()

	required := requiredOtelVersion(t)
	goMod := vendorModesGoMod + "\nrequire " + otelModule + " " + oldOtelVersion + "\n"
	appDir := newVendoredGinApp(t, goMod,
		vendorModesMain(`"`+otelModule+`"`, `"%T", otel.GetTracerProvider()`)).dir

	modulesTxt, err := os.ReadFile(filepath.Join(appDir, "vendor", "modules.txt"))
	require.NoError(t, err)
	require.Contains(t, string(modulesTxt), "# "+otelModule+" "+oldOtelVersion+"\n",
		"the fixture must vendor the old otel version")

	before := moduleFiles(t, appDir)
	out, err := otelcGoBuild(t, appDir, vendorTestEnv())
	assert.Equal(t, before, moduleFiles(t, appDir), "go.mod, go.sum and vendor/ must not change")
	requireNoModModLeak(t, appDir)
	require.Error(t, err, "otelc go build must fail on a version conflict; output:\n%s", out)
	assert.Contains(t, out, otelModule)
	assert.Contains(t, out, oldOtelVersion)
	assert.Contains(t, out, required)
}
