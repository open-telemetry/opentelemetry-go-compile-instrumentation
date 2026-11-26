// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestVendoredBuild(t *testing.T) {
	t.Parallel()

	app := copyTestApp(t, "vendored")

	goMod(t, app, "vendor")
	modulesTxt := filepath.Join(app, "vendor", "modules.txt")
	before, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)

	// otel.instrumentation.go exists and its imports are vendored, so otelc
	// preserves -mod=vendor and runs 100% offline with GOPROXY=off and an empty
	// module cache.
	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)
	emptyCache := t.TempDir()
	buildCmd := exec.CommandContext(t.Context(), otelc, "go", "build", "-mod=vendor", "-o", appBinaryName(), ".")
	buildCmd.Dir = app
	buildCmd.Env = append(os.Environ(), "GOPROXY=off", "GOMODCACHE="+emptyCache)
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, string(buildOut))

	binPath := filepath.Join(app, appBinaryName())
	cmd := exec.CommandContext(t.Context(), binPath)
	cmd.Dir = app
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "vendored hook executed")
	require.Contains(t, string(out), "rate limit: 1")

	after, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	require.Equal(t, before, after, "otelc must not modify the vendor directory")
}

func TestVendoredBuild_FallbackLeavesVendorUntouched(t *testing.T) {
	t.Parallel()

	app := copyTestApp(t, "vendored")
	goMod(t, app, "vendor")

	// Remove otel.instrumentation.go so the project has vendored dependencies
	// without pinned instrumentation imports.
	require.NoError(t, os.Remove(filepath.Join(app, "otel.instrumentation.go")))
	modulesTxt := filepath.Join(app, "vendor", "modules.txt")
	before, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)

	appsDir := filepath.Dir(app)
	vendoredApp := filepath.Base(app)
	testutil.Build(t, appsDir, vendoredApp, "-rules="+filepath.Join(app, "hooks", "otelc.yaml"), "go", "build")

	out, err := exec.CommandContext(t.Context(), filepath.Join(app, appBinaryName())).CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "vendored hook executed")

	after, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	require.Equal(t, before, after, "fallback build must not modify vendor/modules.txt")
}

func TestVendoredBuild_UnvendoredToolImportForcesModMod(t *testing.T) {
	t.Parallel()

	app := copyTestApp(t, "vendored")
	// Vendor only golang.org/x/time by removing otel.instrumentation.go first,
	// then restore otel.instrumentation.go so its import is NOT in vendor/modules.txt.
	toolPath := filepath.Join(app, "otel.instrumentation.go")
	toolBytes, err := os.ReadFile(toolPath)
	require.NoError(t, err)
	require.NoError(t, os.Remove(toolPath))
	goMod(t, app, "vendor")
	require.NoError(t, os.WriteFile(toolPath, toolBytes, 0o644))

	modulesTxt := filepath.Join(app, "vendor", "modules.txt")
	before, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	require.NotContains(t, string(before), "\ngo.opentelemetry.io/otelc/test/apps/vendored/hooks\n")

	appsDir := filepath.Dir(app)
	vendoredApp := filepath.Base(app)
	testutil.Build(t, appsDir, vendoredApp, "go", "build", "-mod=vendor")

	out, err := exec.CommandContext(t.Context(), filepath.Join(app, appBinaryName())).CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "vendored hook executed")

	after, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	require.Equal(t, before, after, "fallback build must not modify vendor/modules.txt")
}

func TestVendoredBuild_VersionGatedAndAddFileOffline(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	depDir := filepath.Join(root, "dep")
	instDir := filepath.Join(root, "inst")
	appDir := filepath.Join(root, "app")
	require.NoError(t, os.MkdirAll(depDir, 0o755))
	require.NoError(t, os.MkdirAll(instDir, 0o755))
	require.NoError(t, os.MkdirAll(appDir, 0o755))

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	pkgDir := filepath.Join(repoRoot, "pkg")

	require.NoError(
		t,
		os.WriteFile(filepath.Join(depDir, "go.mod"), []byte("module example.com/dep\n\ngo 1.25.0\n"), 0o644),
	)
	require.NoError(t, os.WriteFile(
		filepath.Join(depDir, "dep.go"),
		[]byte("package dep\n\nimport \"fmt\"\n\nfunc Greet() { fmt.Println(\"dep greet\") }\n"),
		0o644,
	))

	instGoMod := "module example.com/inst\n\ngo 1.25.0\n\nrequire go.opentelemetry.io/otelc/pkg v0.0.0\n\nreplace go.opentelemetry.io/otelc/pkg => " + pkgDir + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(instDir, "go.mod"), []byte(instGoMod), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(instDir, "hooks.go"),
		[]byte(
			"package inst\n\nimport (\n\t\"fmt\"\n\t\"go.opentelemetry.io/otelc/pkg/hook\"\n)\n\nfunc BeforeGreet(ctx hook.HookContext) {\n\t_ = ctx\n\tfmt.Println(\"versioned hook executed\")\n}\n",
		),
		0o644,
	))
	// //go:build tools + //go:embed ensures go mod vendor copies added.go even though it is //go:build ignore.
	require.NoError(t, os.WriteFile(
		filepath.Join(instDir, "embed.go"),
		[]byte("//go:build tools\n\npackage inst\n\nimport _ \"embed\"\n\n//go:embed added.go\nvar _ string\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(instDir, "added.go"),
		[]byte(
			"//go:build ignore\n\npackage main\n\nimport \"fmt\"\n\nfunc init() { fmt.Println(\"added file executed\") }\n",
		),
		0o644,
	))
	rulesYAML := `version: "v1.0.0"

greet_hook:
  target: example.com/dep
  version: "v1.2.0"
  where:
    func: Greet
  do:
    - inject_hooks:
        before: BeforeGreet
        path: example.com/inst

main_add_file:
  target: main
  do:
    - add_file:
        file: added.go
        path: example.com/inst
`
	require.NoError(t, os.WriteFile(filepath.Join(instDir, "inst.otelc.yml"), []byte(rulesYAML), 0o644))

	appGoMod := `module example.com/app

go 1.25.0

require (
	example.com/dep v1.2.3
	example.com/inst v0.1.0
)

require go.opentelemetry.io/otelc/pkg v0.0.0 // indirect

replace (
	example.com/dep v1.2.3 => ../dep
	example.com/inst v0.1.0 => ../inst
	go.opentelemetry.io/otelc/pkg => ` + pkgDir + `
)
`
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(appGoMod), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(appDir, "main.go"),
		[]byte("package main\n\nimport \"example.com/dep\"\n\nfunc main() { dep.Greet() }\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(appDir, "otel.instrumentation.go"),
		[]byte("//go:build tools\n\npackage tools\n\nimport _ \"example.com/inst\"\n"),
		0o644,
	))

	goMod(t, appDir, "vendor")

	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)
	emptyCache := t.TempDir()
	buildCmd := exec.CommandContext(t.Context(), otelc, "go", "build", "-mod=vendor", "-o", appBinaryName(), ".")
	buildCmd.Dir = appDir
	buildCmd.Env = append(os.Environ(), "GOPROXY=off", "GOMODCACHE="+emptyCache)
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, string(buildOut))

	runOut, err := exec.CommandContext(t.Context(), filepath.Join(appDir, appBinaryName())).CombinedOutput()
	require.NoError(t, err, string(runOut))
	require.Contains(t, string(runOut), "added file executed")
	require.Contains(t, string(runOut), "versioned hook executed")
	require.Contains(t, string(runOut), "dep greet")
}

func writeMockOtelcModule(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tool", "cmd", "otelc"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module go.opentelemetry.io/otelc\n\ngo 1.25.0\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "tool", "cmd", "otelc", "main.go"),
		[]byte("package main\n\nfunc main() {}\n"),
		0o644,
	))
}

func TestVendoredPin_AutoVendors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	instDir := filepath.Join(root, "inst")
	otelcDir := filepath.Join(root, "otelc")
	appDir := filepath.Join(root, "app")
	require.NoError(t, os.MkdirAll(instDir, 0o755))
	require.NoError(t, os.MkdirAll(appDir, 0o755))

	require.NoError(
		t,
		os.WriteFile(filepath.Join(instDir, "go.mod"), []byte("module example.com/inst\n\ngo 1.25.0\n"), 0o644),
	)
	require.NoError(t, os.WriteFile(filepath.Join(instDir, "inst.go"), []byte("package inst\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(instDir, "otelc.yml"),
		[]byte("version: \"v1.0.0\"\n\nr:\n  target: main\n  do:\n    - inject_code:\n        before: \"_ = 1\"\n"),
		0o644,
	))

	writeMockOtelcModule(t, otelcDir)

	appGoMod := `module example.com/app

go 1.25.0

tool go.opentelemetry.io/otelc/tool/cmd/otelc

require (
	example.com/inst v0.1.0
	go.opentelemetry.io/otelc v0.0.0
)

replace (
	example.com/inst v0.1.0 => ../inst
	go.opentelemetry.io/otelc => ../otelc
)
`
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(appGoMod), 0o644))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(appDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644),
	)
	require.NoError(t, os.WriteFile(
		filepath.Join(appDir, "otel.instrumentation.go"),
		[]byte("//go:build tools\n\npackage tools\n\nimport _ \"example.com/inst\"\n"),
		0o644,
	))

	// Create an empty vendor/ directory (stale relative to otel.instrumentation.go).
	require.NoError(t, os.MkdirAll(filepath.Join(appDir, "vendor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "vendor", "modules.txt"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "go.sum"), []byte(""), 0o644))

	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), otelc, "pin")
	cmd.Dir = appDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, strings.ToLower(string(out)), "vendor")

	modulesTxt, err := os.ReadFile(filepath.Join(appDir, "vendor", "modules.txt"))
	require.NoError(t, err)
	require.Contains(t, string(modulesTxt), "example.com/inst")
}

func TestVendoredPin_GeneratesToolFileAndAutoVendors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	depDir := filepath.Join(root, "dep")
	instDir := filepath.Join(root, "inst")
	initDir := filepath.Join(root, "otelinit")
	otelcDir := filepath.Join(root, "otelc")
	appDir := filepath.Join(root, "app")
	for _, d := range []string{depDir, instDir, initDir, appDir} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	const (
		depMod  = "github.com/gin-gonic/gin"
		instMod = "go.opentelemetry.io/otelc-contrib/instrumentation/github.com/gin-gonic/gin/otelcgin"
		initMod = "go.opentelemetry.io/otelc-contrib/instrumentation/go.opentelemetry.io/otel/init"
	)
	require.NoError(t, os.WriteFile(
		filepath.Join(depDir, "go.mod"),
		[]byte("module "+depMod+"\n\ngo 1.25.0\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(filepath.Join(depDir, "dep.go"), []byte("package gin\n"), 0o644))

	ruleYAML := []byte(
		"version: \"v1.0.0\"\n\nr:\n  target: main\n  do:\n    - inject_code:\n        before: \"_ = 1\"\n",
	)
	for _, mod := range []struct {
		dir, path, pkg string
	}{
		{instDir, instMod, "otelcgin"},
		{initDir, initMod, "otelinit"},
	} {
		require.NoError(t, os.WriteFile(
			filepath.Join(mod.dir, "go.mod"),
			[]byte("module "+mod.path+"\n\ngo 1.25.0\n"),
			0o644,
		))
		require.NoError(t, os.WriteFile(filepath.Join(mod.dir, "inst.go"), []byte("package "+mod.pkg+"\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(mod.dir, "otelc.yml"), ruleYAML, 0o644))
	}

	writeMockOtelcModule(t, otelcDir)

	appGoMod := `module example.com/app

go 1.25.0

tool go.opentelemetry.io/otelc/tool/cmd/otelc

require (
	` + depMod + ` v1.10.0
	go.opentelemetry.io/otelc v0.0.0
)

replace (
	` + depMod + ` v1.10.0 => ../dep
	` + instMod + ` => ../inst
	` + initMod + ` => ../otelinit
	go.opentelemetry.io/otelc => ../otelc
)
`
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(appGoMod), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(appDir, "main.go"),
		[]byte("package main\n\nimport _ \""+depMod+"\"\n\nfunc main() {}\n"),
		0o644,
	))

	// Vendor before any tool file exists.
	goMod(t, appDir, "vendor")

	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), otelc, "pin")
	cmd.Dir = appDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "manual edits in vendor/ will be overwritten")

	toolBytes, err := os.ReadFile(filepath.Join(appDir, "otel.instrumentation.go"))
	require.NoError(t, err)
	require.Contains(t, string(toolBytes), instMod)

	modulesTxt, err := os.ReadFile(filepath.Join(appDir, "vendor", "modules.txt"))
	require.NoError(t, err)
	require.Contains(t, string(modulesTxt), instMod)
}

func TestVendoredWorkspaceBuild(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	depDir := filepath.Join(root, "dep")
	instDir := filepath.Join(root, "inst")
	wsDir := filepath.Join(root, "ws")
	appDir := filepath.Join(wsDir, "app")
	libDir := filepath.Join(wsDir, "lib")
	libHooksDir := filepath.Join(libDir, "hooks")
	for _, d := range []string{depDir, instDir, appDir, libHooksDir} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	pkgDir := filepath.Join(repoRoot, "pkg")

	require.NoError(
		t,
		os.WriteFile(filepath.Join(depDir, "go.mod"), []byte("module example.com/dep\n\ngo 1.25.0\n"), 0o644),
	)
	require.NoError(t, os.WriteFile(
		filepath.Join(depDir, "dep.go"),
		[]byte("package dep\n\nimport \"fmt\"\n\nfunc Greet() { fmt.Println(\"dep greet\") }\n"),
		0o644,
	))

	instGoMod := "module example.com/inst\n\ngo 1.25.0\n\nrequire go.opentelemetry.io/otelc/pkg v0.0.0\n\nreplace go.opentelemetry.io/otelc/pkg => " + pkgDir + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(instDir, "go.mod"), []byte(instGoMod), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(instDir, "hooks.go"),
		[]byte(
			"package inst\n\nimport (\n\t\"fmt\"\n\t\"go.opentelemetry.io/otelc/pkg/hook\"\n)\n\nfunc BeforeGreet(ctx hook.HookContext) {\n\t_ = ctx\n\tfmt.Println(\"versioned hook executed\")\n}\n",
		),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(instDir, "embed.go"),
		[]byte("//go:build tools\n\npackage inst\n\nimport _ \"embed\"\n\n//go:embed added.go\nvar _ string\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(instDir, "added.go"),
		[]byte(
			"//go:build ignore\n\npackage main\n\nimport \"fmt\"\n\nfunc init() { fmt.Println(\"added file executed\") }\n",
		),
		0o644,
	))
	instRules := `version: "v1.0.0"

greet_hook:
  target: example.com/dep
  version: "v1.2.0"
  where:
    func: Greet
  do:
    - inject_hooks:
        before: BeforeGreet
        path: example.com/inst

main_add_file:
  target: main
  do:
    - add_file:
        file: added.go
        path: example.com/inst
`
	require.NoError(t, os.WriteFile(filepath.Join(instDir, "inst.otelc.yml"), []byte(instRules), 0o644))

	libGoMod := "module example.com/lib\n\ngo 1.25.0\n\nrequire go.opentelemetry.io/otelc/pkg v0.0.0\n\nreplace go.opentelemetry.io/otelc/pkg => " + pkgDir + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "go.mod"), []byte(libGoMod), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(libDir, "lib.go"),
		[]byte("package lib\n\nimport \"fmt\"\n\nfunc Helper() { fmt.Println(\"lib helper\") }\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(libHooksDir, "hooks.go"),
		[]byte(
			"package hooks\n\nimport (\n\t\"fmt\"\n\t\"go.opentelemetry.io/otelc/pkg/hook\"\n)\n\nfunc BeforeHelper(ctx hook.HookContext) {\n\t_ = ctx\n\tfmt.Println(\"workspace hook executed\")\n}\n",
		),
		0o644,
	))
	libRules := `version: "v1.0.0"

lib_hook:
  target: example.com/lib
  where:
    func: Helper
  do:
    - inject_hooks:
        before: BeforeHelper
        path: example.com/lib/hooks
`
	require.NoError(t, os.WriteFile(filepath.Join(libHooksDir, "lib.otelc.yml"), []byte(libRules), 0o644))

	appGoMod := `module example.com/app

go 1.25.0

require (
	example.com/dep v1.2.3
	example.com/inst v0.1.0
)

require go.opentelemetry.io/otelc/pkg v0.0.0 // indirect

replace (
	example.com/dep v1.2.3 => ../../dep
	example.com/inst v0.1.0 => ../../inst
	go.opentelemetry.io/otelc/pkg => ` + pkgDir + `
)
`
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(appGoMod), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(appDir, "main.go"),
		[]byte(
			"package main\n\nimport (\n\t\"example.com/dep\"\n\t\"example.com/lib\"\n)\n\nfunc main() {\n\tdep.Greet()\n\tlib.Helper()\n}\n",
		),
		0o644,
	))
	// app/otel.instrumentation.go imports both external vendored inst and sibling workspace lib/hooks.
	require.NoError(t, os.WriteFile(
		filepath.Join(appDir, "otel.instrumentation.go"),
		[]byte(
			"//go:build tools\n\npackage tools\n\nimport (\n\t_ \"example.com/inst\"\n\t_ \"example.com/lib/hooks\"\n)\n",
		),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(wsDir, "go.work"),
		[]byte("go 1.25.0\n\nuse (\n\t./app\n\t./lib\n)\n"),
		0o644,
	))

	goWork(t, wsDir, "vendor")
	modulesTxt := filepath.Join(wsDir, "vendor", "modules.txt")
	before, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	require.Contains(t, string(before), "example.com/inst")
	require.NotContains(t, string(before), "example.com/lib")

	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)
	emptyCache := t.TempDir()
	binPath := filepath.Join(t.TempDir(), appBinaryName())
	buildCmd := exec.CommandContext(t.Context(), otelc, "go", "build", "-mod=vendor", "-o", binPath, "./app")
	buildCmd.Dir = wsDir
	buildCmd.Env = append(os.Environ(), "GOWORK=", "GOPROXY=off", "GOMODCACHE="+emptyCache)
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, string(buildOut))

	runOut, err := exec.CommandContext(t.Context(), binPath).CombinedOutput()
	require.NoError(t, err, string(runOut))
	require.Contains(t, string(runOut), "added file executed")
	require.Contains(t, string(runOut), "versioned hook executed")
	require.Contains(t, string(runOut), "workspace hook executed")
	require.Contains(t, string(runOut), "dep greet")
	require.Contains(t, string(runOut), "lib helper")

	after, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	require.Equal(t, before, after, "otelc must not modify workspace vendor/modules.txt")
}

func TestVendoredWorkspaceBuild_MultiToolFilesOffline(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	instADir := filepath.Join(root, "instA")
	instBDir := filepath.Join(root, "instB")
	wsDir := filepath.Join(root, "ws")
	appADir := filepath.Join(wsDir, "appA")
	appBDir := filepath.Join(wsDir, "appB")
	for _, d := range []string{instADir, instBDir, appADir, appBDir} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	pkgDir := filepath.Join(repoRoot, "pkg")

	for _, inst := range []struct {
		dir     string
		modPath string
		msg     string
	}{
		{instADir, "example.com/instA", "instA hook executed"},
		{instBDir, "example.com/instB", "instB hook executed"},
	} {
		goMod := "module " + inst.modPath + "\n\ngo 1.25.0\n\nrequire go.opentelemetry.io/otelc/pkg v0.0.0\n\nreplace go.opentelemetry.io/otelc/pkg => " + pkgDir + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(inst.dir, "go.mod"), []byte(goMod), 0o644))
		require.NoError(t, os.WriteFile(
			filepath.Join(inst.dir, "hooks.go"),
			[]byte(
				"package hooks\n\nimport (\n\t\"fmt\"\n\t\"go.opentelemetry.io/otelc/pkg/hook\"\n)\n\nfunc BeforeMain(ctx hook.HookContext) {\n\t_ = ctx\n\tfmt.Println(\""+inst.msg+"\")\n}\n",
			),
			0o644,
		))
		rules := "version: \"v1.0.0\"\n\nh:\n  target: main\n  where:\n    func: Run\n  do:\n    - inject_hooks:\n        before: BeforeMain\n        path: " + inst.modPath + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(inst.dir, "otelc.yml"), []byte(rules), 0o644))
	}

	writeApp := func(dir, name, instMod, instRel string) {
		goMod := "module " + name + "\n\ngo 1.25.0\n\nrequire (\n\t" + instMod + " v0.1.0\n\tgo.opentelemetry.io/otelc/pkg v0.0.0\n)\n\nreplace (\n\t" + instMod + " v0.1.0 => " + instRel + "\n\tgo.opentelemetry.io/otelc/pkg => " + pkgDir + "\n)\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "main.go"),
			[]byte("package main\n\nfunc Run() {}\n\nfunc main() { Run() }\n"),
			0o644,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "otel.instrumentation.go"),
			[]byte("//go:build tools\n\npackage tools\n\nimport _ \""+instMod+"\"\n"),
			0o644,
		))
	}
	writeApp(appADir, "example.com/appA", "example.com/instA", "../../instA")
	writeApp(appBDir, "example.com/appB", "example.com/instB", "../../instB")
	require.NoError(t, os.WriteFile(
		filepath.Join(wsDir, "go.work"),
		[]byte("go 1.25.0\n\nuse (\n\t./appA\n\t./appB\n)\n"),
		0o644,
	))

	goWork(t, wsDir, "vendor")

	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)
	emptyCache := t.TempDir()
	outDir := t.TempDir()
	buildCmd := exec.CommandContext(
		t.Context(),
		otelc,
		"go",
		"build",
		"-o",
		outDir+string(filepath.Separator),
		"./appA",
		"./appB",
	)
	buildCmd.Dir = wsDir
	buildCmd.Env = append(os.Environ(), "GOWORK=", "GOPROXY=off", "GOMODCACHE="+emptyCache)
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, string(buildOut))

	outA, err := exec.CommandContext(t.Context(), filepath.Join(outDir, binaryName("appA"))).CombinedOutput()
	require.NoError(t, err, string(outA))
	require.Contains(t, string(outA), "instA hook executed")

	outB, err := exec.CommandContext(t.Context(), filepath.Join(outDir, binaryName("appB"))).CombinedOutput()
	require.NoError(t, err, string(outB))
	require.Contains(t, string(outB), "instB hook executed")
}

func TestVendoredWorkspaceBuild_FallbackReadonlyLeavesVendorUntouched(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	depDir := filepath.Join(root, "dep")
	wsDir := filepath.Join(root, "ws")
	appDir := filepath.Join(wsDir, "app")
	hooksDir := filepath.Join(appDir, "hooks")
	for _, d := range []string{depDir, hooksDir} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	pkgDir := filepath.Join(repoRoot, "pkg")

	require.NoError(
		t,
		os.WriteFile(filepath.Join(depDir, "go.mod"), []byte("module example.com/dep\n\ngo 1.25.0\n"), 0o644),
	)
	require.NoError(t, os.WriteFile(
		filepath.Join(depDir, "dep.go"),
		[]byte("package dep\n\nimport \"fmt\"\n\nfunc Greet() { fmt.Println(\"dep greet\") }\n"),
		0o644,
	))

	appGoMod := `module example.com/app

go 1.25.0

require (
	example.com/dep v1.2.3
	go.opentelemetry.io/otelc/pkg v0.0.0
)

replace (
	example.com/dep v1.2.3 => ../../dep
	go.opentelemetry.io/otelc/pkg => ` + pkgDir + `
)
`
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(appGoMod), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(appDir, "main.go"),
		[]byte("package main\n\nimport \"example.com/dep\"\n\nfunc main() { dep.Greet() }\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(hooksDir, "hooks.go"),
		[]byte(
			"package hooks\n\nimport (\n\t\"fmt\"\n\t\"go.opentelemetry.io/otelc/pkg/hook\"\n)\n\nfunc BeforeGreet(ctx hook.HookContext) {\n\t_ = ctx\n\tfmt.Println(\"readonly workspace fallback hook\")\n}\n",
		),
		0o644,
	))
	rulesYAML := `version: "v1.0.0"

greet_hook:
  target: example.com/dep
  where:
    func: Greet
  do:
    - inject_hooks:
        before: BeforeGreet
        path: example.com/app/hooks
`
	rulesPath := filepath.Join(hooksDir, "otelc.yaml")
	require.NoError(t, os.WriteFile(rulesPath, []byte(rulesYAML), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(wsDir, "go.work"),
		[]byte("go 1.25.0\n\nuse ./app\n"),
		0o644,
	))

	goWork(t, wsDir, "vendor")
	modulesTxt := filepath.Join(wsDir, "vendor", "modules.txt")
	before, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)

	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)
	binPath := filepath.Join(t.TempDir(), appBinaryName())
	// No tool file exists, so otelc forces -mod=readonly and leaves vendor/ untouched.
	buildCmd := exec.CommandContext(
		t.Context(),
		otelc,
		"-rules="+rulesPath,
		"go",
		"build",
		"-mod=vendor",
		"-o",
		binPath,
		"./app",
	)
	buildCmd.Dir = wsDir
	buildCmd.Env = append(os.Environ(), "GOWORK=")
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, string(buildOut))

	runOut, err := exec.CommandContext(t.Context(), binPath).CombinedOutput()
	require.NoError(t, err, string(runOut))
	require.Contains(t, string(runOut), "readonly workspace fallback hook")
	require.Contains(t, string(runOut), "dep greet")

	after, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	require.Equal(t, before, after, "fallback workspace build must not modify vendor/modules.txt")
}

func TestVendoredWorkspaceBuild_OfflineUnpinnedDiagnostic(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	proxyDir := filepath.Join(root, "proxy")
	cacheDir := filepath.Join(root, "modcache")
	wsDir := filepath.Join(root, "ws")
	appDir := filepath.Join(wsDir, "app")
	require.NoError(t, os.MkdirAll(appDir, 0o755))

	writeProxyModule(t, proxyDir, "example.com/offdep", "v1.0.0", map[string]string{
		"go.mod": "module example.com/offdep\n\ngo 1.25.0\n",
		"dep.go": "package offdep\nfunc Hi() {}\n",
	})
	require.NoError(t, os.WriteFile(
		filepath.Join(appDir, "go.mod"),
		[]byte("module example.com/app\n\ngo 1.25.0\n\nrequire example.com/offdep v1.0.0\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(appDir, "main.go"),
		[]byte("package main\nimport \"example.com/offdep\"\nfunc main() { offdep.Hi() }\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(wsDir, "go.work"),
		[]byte("go 1.25.0\n\nuse ./app\n"),
		0o644,
	))

	proxyURL := "file://" + filepath.ToSlash(proxyDir)
	t.Cleanup(func() {
		cleanCmd := exec.Command("go", "clean", "-modcache")
		cleanCmd.Env = append(os.Environ(), "GOMODCACHE="+cacheDir)
		_ = cleanCmd.Run()
	})
	tidyCmd := exec.CommandContext(t.Context(), "go", "mod", "tidy")
	tidyCmd.Dir = appDir
	tidyCmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY="+proxyURL, "GOSUMDB=off", "GOMODCACHE="+cacheDir)
	tidyOut, err := tidyCmd.CombinedOutput()
	require.NoError(t, err, string(tidyOut))

	vendorCmd := exec.CommandContext(t.Context(), "go", "work", "vendor")
	vendorCmd.Dir = wsDir
	vendorCmd.Env = append(os.Environ(), "GOWORK=", "GOPROXY="+proxyURL, "GOSUMDB=off", "GOMODCACHE="+cacheDir)
	vendorOut, err := vendorCmd.CombinedOutput()
	require.NoError(t, err, string(vendorOut))

	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)
	emptyCache := t.TempDir()
	buildCmd := exec.CommandContext(t.Context(), otelc, "go", "build", "-mod=vendor", "-o", appBinaryName(), "./app")
	buildCmd.Dir = wsDir
	buildCmd.Env = append(os.Environ(), "GOWORK=", "GOPROXY=off", "GOSUMDB=off", "GOMODCACHE="+emptyCache)
	buildOut, err := buildCmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(buildOut), "otelc pin")
	require.Contains(t, string(buildOut), "go work vendor")
	require.Contains(t, string(buildOut), "GOWORK=off")
}

func TestVendoredWorkspacePin_AutoVendors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	instDir := filepath.Join(root, "inst")
	otelcDir := filepath.Join(root, "otelc")
	wsDir := filepath.Join(root, "ws")
	app1Dir := filepath.Join(wsDir, "app1")
	app2Dir := filepath.Join(wsDir, "app2")
	for _, d := range []string{
		instDir,
		filepath.Join(otelcDir, "tool", "cmd", "otelc"),
		filepath.Join(wsDir, "vendor"),
		app1Dir,
		app2Dir,
	} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	require.NoError(
		t,
		os.WriteFile(filepath.Join(instDir, "go.mod"), []byte("module example.com/inst\n\ngo 1.25.0\n"), 0o644),
	)
	require.NoError(t, os.WriteFile(filepath.Join(instDir, "inst.go"), []byte("package inst\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(instDir, "otelc.yml"),
		[]byte(
			"version: \"v1.0.0\"\n\nr:\n  target: main\n  where:\n    func: main\n  do:\n    - inject_code:\n        raw: \"println(\\\"ws pin hook\\\")\"\n",
		),
		0o644,
	))

	require.NoError(t, os.WriteFile(
		filepath.Join(otelcDir, "go.mod"),
		[]byte("module go.opentelemetry.io/otelc\n\ngo 1.25.0\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(otelcDir, "tool", "cmd", "otelc", "main.go"),
		[]byte("package main\n\nfunc main() {}\n"),
		0o644,
	))

	for _, mod := range []struct {
		dir  string
		path string
	}{
		{app1Dir, "example.com/app1"},
		{app2Dir, "example.com/app2"},
	} {
		appGoMod := "module " + mod.path + "\n\ngo 1.25.0\n\n" +
			"tool go.opentelemetry.io/otelc/tool/cmd/otelc\n\n" +
			"require (\n\texample.com/inst v0.1.0\n\tgo.opentelemetry.io/otelc v0.0.0\n)\n\n" +
			"replace (\n\texample.com/inst v0.1.0 => ../../inst\n\tgo.opentelemetry.io/otelc => ../../otelc\n)\n"
		require.NoError(t, os.WriteFile(filepath.Join(mod.dir, "go.mod"), []byte(appGoMod), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(mod.dir, "go.sum"), []byte(""), 0o644))
		require.NoError(
			t,
			os.WriteFile(filepath.Join(mod.dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644),
		)
		require.NoError(t, os.WriteFile(
			filepath.Join(mod.dir, "otel.instrumentation.go"),
			[]byte("//go:build tools\n\npackage tools\n\nimport _ \"example.com/inst\"\n"),
			0o644,
		))
	}

	require.NoError(t, os.WriteFile(
		filepath.Join(wsDir, "go.work"),
		[]byte("go 1.25.0\n\nuse (\n\t./app1\n\t./app2\n)\n"),
		0o644,
	))
	// Stale/empty workspace vendor/modules.txt.
	modulesTxt := filepath.Join(wsDir, "vendor", "modules.txt")
	require.NoError(t, os.WriteFile(modulesTxt, []byte("## workspace\n"), 0o644))

	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), otelc, "pin", "./app1", "./app2")
	cmd.Dir = wsDir
	cmd.Env = append(os.Environ(), "GOWORK=")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Equal(t, 1, strings.Count(string(out), "go work vendor"), string(out))
	require.Contains(t, strings.ToLower(string(out)), "vendor")

	modulesTxtBytes, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	require.Contains(t, string(modulesTxtBytes), "example.com/inst")

	// Subsequent instrumented build in the workspace runs completely offline.
	emptyCache := t.TempDir()
	binPath := filepath.Join(t.TempDir(), appBinaryName())
	buildCmd := exec.CommandContext(t.Context(), otelc, "go", "build", "-mod=vendor", "-o", binPath, "./app1")
	buildCmd.Dir = wsDir
	buildCmd.Env = append(os.Environ(), "GOWORK=", "GOPROXY=off", "GOMODCACHE="+emptyCache)
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, string(buildOut))

	runOut, err := exec.CommandContext(t.Context(), binPath).CombinedOutput()
	require.NoError(t, err, string(runOut))
	require.Contains(t, string(runOut), "ws pin hook")
}

func binaryName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func appBinaryName() string {
	return binaryName("app")
}

func goMod(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", append([]string{"mod"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func goWork(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", append([]string{"work"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func writeProxyModule(t *testing.T, proxyDir, modPath, ver string, files map[string]string) {
	t.Helper()
	vDir := filepath.Join(proxyDir, filepath.FromSlash(modPath), "@v")
	require.NoError(t, os.MkdirAll(vDir, 0o755))
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(vDir, ver+".info"),
			[]byte(`{"Version":"`+ver+`","Time":"2026-01-01T00:00:00Z"}`),
			0o644,
		),
	)
	require.NoError(t, os.WriteFile(filepath.Join(vDir, ver+".mod"), []byte(files["go.mod"]), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vDir, "list"), []byte(ver+"\n"), 0o644))

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(modPath + "@" + ver + "/" + name)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, os.WriteFile(filepath.Join(vDir, ver+".zip"), buf.Bytes(), 0o644))
}
