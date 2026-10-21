//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"

	"go.opentelemetry.io/otelc/test/testutil"
)

// The tests in this file define what otelc does when a dependency of the hook
// code (not the hook code itself) needs a higher version of a vendored
// module:
//   - `otelc go build` fails with a clear message, and changes nothing.
//   - `otelc vendor` gets the higher versions, like "go get" and
//     "go mod vendor" do, and tells each change.

// hookDepModule is a module that gin vendors at a lower version than the one
// that a dependency of the hook code (grpc) needs.
const (
	hookDepModule        = "golang.org/x/net"
	hookDepRequiredBy    = "google.golang.org/grpc"
	hookDepMinVersion    = "v0.58.0"
	vendorModulesTxtPath = "vendor/modules.txt"
)

// hookDepVersions are the lowest versions of the vendored modules of the gin
// fixture that the dependencies of the hook code need. A fixture that vendors
// them builds without a vendor conflict.
//
//nolint:gochecknoglobals // test data
var hookDepVersions = []string{
	hookDepModule + "@" + hookDepMinVersion,
	"golang.org/x/sys@v0.47.0",
	"google.golang.org/protobuf@v1.36.12",
}

// goGetHookDeps runs "go get" in dir for hookDepVersions, so that a later
// "go mod vendor" vendors versions that the hook code can use.
func goGetHookDeps(t *testing.T, dir string, env []string) {
	t.Helper()
	runGo(t, dir, env, append([]string{"get"}, hookDepVersions...)...)
}

// newOldDepsGinApp writes the gin app and vendors its dependencies at the
// versions that gin needs: some of them are lower than the versions that the
// dependencies of the hook code need.
func newOldDepsGinApp(t *testing.T) vendorFixture {
	t.Helper()
	appsDir := t.TempDir()
	appDir := filepath.Join(appsDir, vendorModesApp)
	writeTestFile(t, appDir, "go.mod", vendorModesGoMod)
	writeTestFile(t, appDir, "main.go", vendorModesMain("", `"ok"`))

	env := vendorTestEnv()
	runGo(t, appDir, env, "mod", "tidy")
	runGo(t, appDir, env, "mod", "vendor")
	return vendorFixture{appsDir: appsDir, dir: appDir}
}

// otelcBinary returns the absolute path of the otelc binary.
func otelcBinary(t *testing.T) string {
	t.Helper()
	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelc, err = filepath.Abs(otelc)
	require.NoError(t, err)
	return otelc
}

// vendoredVersion returns the version of module in vendor/modules.txt.
func vendoredVersion(t *testing.T, appDir, module string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(appDir, filepath.FromSlash(vendorModulesTxtPath)))
	require.NoError(t, err)
	m := regexp.MustCompile(`(?m)^# ` + regexp.QuoteMeta(module) + ` (v\S+)`).FindSubmatch(data)
	require.NotNil(t, m, "vendor/modules.txt must have %s", module)
	return string(m[1])
}

// TestVendoredTransitiveConflict: grpc (a dependency of the hook code) needs a
// higher version of golang.org/x/net than the vendored one. The build must
// fail, tell the module, the versions, the requiring module and the fix, and
// change no file of the project.
func TestVendoredTransitiveConflict(t *testing.T) {
	t.Parallel()

	appDir := newOldDepsGinApp(t).dir
	vendored := vendoredVersion(t, appDir, hookDepModule)
	require.Negative(t, semver.Compare(vendored, hookDepMinVersion),
		"the fixture must vendor an old %s", hookDepModule)

	before := sourceTree(t, appDir)
	out, err := otelcGoBuild(t, appDir, vendorTestEnv())
	assert.Equal(t, before, sourceTree(t, appDir), "the failed build must not change the project files")
	require.Error(t, err, "otelc go build must fail on a transitive version conflict; output:\n%s", out)
	assert.Contains(t, out, "vendor conflict")
	assert.Contains(t, out, hookDepModule)
	assert.Contains(t, out, vendored)
	assert.Contains(t, out, hookDepMinVersion)
	assert.Contains(t, out, hookDepRequiredBy)
	assert.Contains(t, out, `go get `+hookDepModule+`@`+hookDepMinVersion)
	assert.Contains(t, out, "otelc vendor")
}

// TestVendoredOtelcVendorBumps: `otelc vendor` gets the higher versions that
// the dependencies of the hook code need, and tells each change. Then the
// project builds with no network and an empty module cache, with the go
// command alone and with otelc, also without -mod=vendor in GOFLAGS.
func TestVendoredOtelcVendorBumps(t *testing.T) {
	t.Parallel()

	app := newOldDepsGinApp(t)
	appsDir, appDir := app.appsDir, app.dir
	vendored := vendoredVersion(t, appDir, hookDepModule)

	otelc := otelcBinary(t)
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(appDir, ".otelc-build"))
		_ = os.Remove(filepath.Join(appDir, ".otelc-build.lock"))
	})
	out := runOtelcCommand(t, appDir, vendorTestEnv(), otelc, "vendor")
	assert.Regexp(t, `Bumped dependency `+regexp.QuoteMeta(hookDepModule)+` \(`+regexp.QuoteMeta(vendored)+` -> v\S+\)`,
		out, "otelc vendor must tell the bump")

	bumped := vendoredVersion(t, appDir, hookDepModule)
	assert.GreaterOrEqual(t, semver.Compare(bumped, hookDepMinVersion), 0,
		"vendor/ must have the version that the hook code needs")
	goMod, err := os.ReadFile(filepath.Join(appDir, "go.mod"))
	require.NoError(t, err)
	assert.Regexp(t, `(?m)^\s*`+regexp.QuoteMeta(hookDepModule)+`\s+`+regexp.QuoteMeta(bumped)+`\b`, string(goMod),
		"go.mod must require the bumped version")

	snapshot := sourceTree(t, appDir)

	// The go command alone builds the result with no network, no module
	// cache and no -mod flag.
	env := vendorTestEnv(offlineEnv(newModCache(t))...)
	runGo(t, appDir, env, "build", "-o", "plain.bin", ".")
	require.NoError(t, os.Remove(filepath.Join(appDir, "plain.bin")))

	out, err = otelcGoBuild(t, appDir, env)
	assert.Equal(t, snapshot, sourceTree(t, appDir), "the build must not change the project files")
	require.NoError(t, err, "air-gapped otelc go build failed:\n%s", out)
	requireNoModModLeak(t, appDir)

	requireServerSpan(t, appsDir, "ok")

	// otelc vendor wrote the checksums of the modules that it added, so a
	// standard "go mod vendor" works. It removes the otelc-bundle
	// annotations; a second otelc vendor writes them again.
	runGo(t, appDir, vendorTestEnv(), "mod", "vendor")
	modulesTxt, err := os.ReadFile(filepath.Join(appDir, filepath.FromSlash(vendorModulesTxtPath)))
	require.NoError(t, err)
	require.NotContains(t, string(modulesTxt), "otelc-bundle")
	runOtelcCommand(t, appDir, vendorTestEnv(), otelc, "vendor")
	modulesTxt, err = os.ReadFile(filepath.Join(appDir, filepath.FromSlash(vendorModulesTxtPath)))
	require.NoError(t, err)
	assert.Contains(t, string(modulesTxt), "otelc-bundle ", "otelc vendor writes the bundle fingerprint again")
	runGo(t, appDir, vendorTestEnv(offlineEnv(newModCache(t))...), "build", "-o", "plain.bin", ".")
	require.NoError(t, os.Remove(filepath.Join(appDir, "plain.bin")))
}
