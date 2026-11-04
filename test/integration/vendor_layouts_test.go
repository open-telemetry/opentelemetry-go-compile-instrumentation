//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

// The tests in this file build vendored apps with local instrumentation:
// instrumentation code of the user, not of otelc. otel.instrumentation.go
// imports it with the instrumentation of otelc. The local instrumentation
// can be:
//   - a package of the main module.
//   - a nested module that go.mod replaces with a local directory (like
//     test/apps/gincustom).
//
// The hook of the local instrumentation adds the attribute
// localInstrumentationAttr to the server span.

const (
	localInstrumentationAttr = "vendmodes.local"
	// localAppModule is the module path of the app. It has a dot, like a
	// real module path: the go command and otelc do not take its packages
	// for packages of the standard library.
	localAppModule = "example.com/" + vendorModesApp
	// localToolFileImports are the otelc imports of otel.instrumentation.go.
	localToolFileImports = `	_ "go.opentelemetry.io/otelc/instrumentation/go.opentelemetry.io/otel/init"
	_ "go.opentelemetry.io/otelc/instrumentation/net/http/server"
`
)

// localHookSource returns the hook code of the local instrumentation, in the
// package pkgName.
func localHookSource(pkgName string) string {
	return `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ` + pkgName + `

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func afterNew(_ hook.HookContext, r *gin.Engine) {
	r.Use(func(c *gin.Context) {
		trace.SpanFromContext(c.Request.Context()).SetAttributes(attribute.Bool("` + localInstrumentationAttr + `", true))
		c.Next()
	})
}
`
}

// localRules returns the rule file of the local instrumentation: the hook
// package is hookPath.
func localRules(hookPath string) string {
	return `version: "v1.0.0"

local_gin:
  target: github.com/gin-gonic/gin
  where:
    func: New
  do:
    - inject_hooks:
        after: "afterNew"
        path: "` + hookPath + `"
`
}

// localToolFile returns otel.instrumentation.go with the imports.
func localToolFile(imports string) string {
	return `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build tools

package tools

import (
` + imports + `)
`
}

// otelcPkgReplace returns a replace directive for the otelc pkg module (the
// hook API) of this repository. The module is not published, so the go
// command of the user needs it to vendor the local hook code.
func otelcPkgReplace(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	return "\nrequire go.opentelemetry.io/otelc/pkg v0.0.0-00010101000000-000000000000\n\n" +
		"replace go.opentelemetry.io/otelc/pkg => " + filepath.ToSlash(filepath.Join(repoRoot, "pkg")) + "\n"
}

// vendorLocalApp vendors the app in appDir: otel.instrumentation.go imports
// only localImport for "go mod tidy" and "go mod vendor" (the go command
// cannot get the otelc modules). Then otel.instrumentation.go gets the otelc
// imports too, as when the user adds them. otelc adds the otelc modules.
func vendorLocalApp(t *testing.T, appDir, localImport string) {
	t.Helper()
	local := "\t_ \"" + localImport + "\"\n"
	writeTestFile(t, appDir, "otel.instrumentation.go", localToolFile(local))
	env := vendorTestEnv()
	runGo(t, appDir, env, "mod", "tidy")
	goGetHookDeps(t, appDir, env)
	runGo(t, appDir, env, "mod", "vendor")
	writeTestFile(t, appDir, "otel.instrumentation.go", localToolFile(localToolFileImports+local))
}

// requireLocalInstrumentation builds the app with otelc, checks that the
// build changes no file of the app, runs the app, and checks that the hook of
// the local instrumentation ran.
func requireLocalInstrumentation(t *testing.T, app vendorFixture) {
	t.Helper()
	before := sourceTree(t, app.dir)
	out, err := otelcGoBuild(t, app.dir, vendorTestEnv())
	require.NoError(t, err, "otelc go build failed:\n%s", out)
	require.Equal(t, before, sourceTree(t, app.dir), "a build changes no file of the app")

	port := testutil.FreePort(t)
	f := testutil.NewTestFixture(t, testutil.WithAppsDir(app.appsDir))
	f.Start(vendorModesApp, fmt.Sprintf("-port=%d", port))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d/hello", port), nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	f.WaitForSpans(1)
	// Only the net/http server instrumentation of otelc is imported, so the
	// span has no route in its name.
	span := testutil.RequireSpan(t, f.Traces(), testutil.IsServer)
	testutil.RequireAttribute(t, span, localInstrumentationAttr, true)
}

// localAppGoMod returns the go.mod file of the gin app, with the module path
// localAppModule.
func localAppGoMod() string {
	return strings.Replace(vendorModesGoMod, "module "+vendorModesApp+"\n", "module "+localAppModule+"\n", 1)
}

// TestVendoredMainModuleInstrumentation: the local instrumentation is the
// package example.com/vendmodes/instr of the main module.
func TestVendoredMainModuleInstrumentation(t *testing.T) {
	t.Parallel()

	appsDir := t.TempDir()
	appDir := filepath.Join(appsDir, vendorModesApp)
	writeTestFile(t, appDir, "go.mod", localAppGoMod()+otelcPkgReplace(t))
	writeTestFile(t, appDir, "main.go", vendorModesMain("", `"ok"`))
	writeTestFile(t, appDir, "instr/hook.go", localHookSource("instr"))
	writeTestFile(t, appDir, "instr/otelc.yaml", localRules(localAppModule+"/instr"))
	vendorLocalApp(t, appDir, localAppModule+"/instr")

	requireLocalInstrumentation(t, vendorFixture{appsDir: appsDir, dir: appDir})
}

// TestVendoredLocalModuleInstrumentation: the local instrumentation is the
// nested module example.com/vendmodes/instrumentation, and go.mod replaces it
// with ./instrumentation. Its rules name the hook package
// example.com/vendmodes/instrumentation/hooks, which vendor/ does not have.
func TestVendoredLocalModuleInstrumentation(t *testing.T) {
	t.Parallel()

	const localModule = localAppModule + "/instrumentation"
	appsDir := t.TempDir()
	appDir := filepath.Join(appsDir, vendorModesApp)
	pkgReplace := otelcPkgReplace(t)
	writeTestFile(t, appDir, "go.mod", localAppGoMod()+pkgReplace+
		"\nrequire "+localModule+" v0.0.0-00010101000000-000000000000\n\n"+
		"replace "+localModule+" => ./instrumentation\n")
	writeTestFile(t, appDir, "main.go", vendorModesMain("", `"ok"`))
	localDir := filepath.Join(appDir, "instrumentation")
	writeTestFile(t, localDir, "go.mod", "module "+localModule+"\n\ngo 1.25.0\n"+pkgReplace)
	// Only the rules name the hook package: "go mod vendor" does not vendor
	// it. otelc takes it from the local directory.
	writeTestFile(t, localDir, "doc.go", "// Package instrumentation has the rules.\npackage instrumentation\n")
	writeTestFile(t, localDir, "otelc.yaml", localRules(localModule+"/hooks"))
	writeTestFile(t, localDir, "hooks/hook.go", localHookSource("hooks"))
	runGo(t, localDir, vendorTestEnv(), "mod", "tidy")
	vendorLocalApp(t, appDir, localModule)
	require.Contains(t, readFile(t, filepath.Join(appDir, "vendor", "modules.txt")),
		"# "+localModule+" v0.0.0-00010101000000-000000000000 => ./instrumentation\n",
		"the fixture vendors the local module")
	require.NoDirExists(t, filepath.Join(appDir, "vendor", filepath.FromSlash(localModule), "hooks"),
		"the fixture does not vendor the hook package")

	requireLocalInstrumentation(t, vendorFixture{appsDir: appsDir, dir: appDir})
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}
