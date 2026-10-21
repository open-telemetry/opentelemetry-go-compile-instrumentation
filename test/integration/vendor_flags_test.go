//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

// The tests in this file check how the flags and the rules of a build change
// the vendor mode of otelc:
//   - A -mod=mod flag makes the go command ignore vendor/. otelc does the
//     same in all its package loads.
//   - Custom rules (OTELC_RULES) can add hook packages from a module that
//     go.mod requires, but that vendor/ does not have. otelc adds them to
//     vendor/ for the time of the build.

// customRuleModule is a module that the gin fixture does not use. A custom
// rule of the tests adds an import of its package.
const (
	customRuleModule  = "github.com/google/uuid"
	customRuleVersion = "v1.6.0"
)

// otelcGoBuildWithArgs runs `otelc go build <buildArgs> -o app .` in appDir
// with env. It returns the combined output and the error.
func otelcGoBuildWithArgs(t *testing.T, appDir string, env []string, buildArgs ...string) (string, error) {
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

	args := append([]string{"go", "build"}, buildArgs...)
	args = append(args, "-o", vendorModesBin(), ".")
	cmd := exec.CommandContext(t.Context(), otelc, args...)
	cmd.Dir = appDir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestVendoredModModBuild: `otelc go build -mod=mod` of a vendored app uses
// module mode. otelc adds the modules of the hook code to go.mod, so a package
// load in vendor mode reports "inconsistent vendoring". All the package loads
// of otelc must use -mod=mod too.
func TestVendoredModModBuild(t *testing.T) {
	t.Parallel()

	app := newVendoredGinApp(t, vendorModesGoMod, vendorModesMain("", `"ok"`))
	appsDir, appDir := app.appsDir, app.dir

	before := moduleFiles(t, appDir)
	out, err := otelcGoBuildWithArgs(t, appDir, vendorTestEnv(), "-mod=mod")
	assert.Equal(t, before, moduleFiles(t, appDir), "go.mod, go.sum and vendor/ must not change")
	require.NoError(t, err, "otelc go build -mod=mod failed:\n%s", out)
	assert.NotContains(t, out, "inconsistent vendoring")

	requireServerSpan(t, appsDir, "ok")
}

// TestVendoredCustomRuleHook: a custom rule adds an import of a package of a
// module that go.mod requires, but that vendor/ does not have ("go mod vendor"
// copies only the packages that the app imports). otelc adds the package to
// vendor/ for the time of the build.
func TestVendoredCustomRuleHook(t *testing.T) {
	t.Parallel()

	app := newVendoredGinApp(t, vendorModesGoMod, vendorModesMain("", `"ok"`))
	appDir := app.dir
	env := vendorTestEnv()
	runGo(t, appDir, env, "get", customRuleModule+"@"+customRuleVersion)
	runGo(t, appDir, env, "mod", "vendor")
	modulesTxt, err := os.ReadFile(filepath.Join(appDir, vendorModulesTxtPath))
	require.NoError(t, err)
	require.Contains(t, string(modulesTxt), "# "+customRuleModule+" "+customRuleVersion+"\n",
		"go.mod requires the module")
	require.NotContains(t, string(modulesTxt), "\n"+customRuleModule+"\n",
		"the fixture must not vendor the package")

	rules := filepath.Join(t.TempDir(), "rules.otelc.yml")
	writeTestFile(t, filepath.Dir(rules), filepath.Base(rules), `add_uuid:
  target: main
  func: main
  raw: "_ = uuid.Nil"
  imports:
    uuid: `+customRuleModule+`
`)

	before := moduleFiles(t, appDir)
	out, err := otelcGoBuildWithArgs(t, appDir, vendorTestEnv("OTELC_RULES="+rules))
	assert.Equal(t, before, moduleFiles(t, appDir), "go.mod, go.sum and vendor/ must not change")
	require.NoError(t, err, "otelc go build with a custom rule failed:\n%s", out)
	requireNoModModLeak(t, appDir)
	assert.NotContains(t, out, "import lookup disabled")
}
