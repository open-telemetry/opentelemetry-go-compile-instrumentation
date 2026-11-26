// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
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

func TestVendoredPin_AutoVendors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	instDir := filepath.Join(root, "inst")
	otelcDir := filepath.Join(root, "otelc")
	appDir := filepath.Join(root, "app")
	require.NoError(t, os.MkdirAll(instDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(otelcDir, "tool", "cmd", "otelc"), 0o755))
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

func appBinaryName() string {
	if runtime.GOOS == "windows" {
		return "app.exe"
	}
	return "app"
}

func goMod(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", append([]string{"mod"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
