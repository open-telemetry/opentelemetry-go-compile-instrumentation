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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

const testOnlyLicenseHeader = `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

`

// testOnlyHookTest is a test file that fails unless the hook on lib.Do ran.
const testOnlyHookTest = `
import (
	"os"
	"testing"

	"example.com/otelc-testonly/instrumentation/lib"
)

func TestHook(t *testing.T) {
	lib.Do()
	if os.Getenv("OTELC_TESTONLY_HOOK") != "ran" {
		t.Fatal("the hook on lib.Do did not run")
	}
}
`

// writeTestOnlyModule writes a module whose tool file selects a hook on lib.Do.
// These packages have only _test.go files:
//   - inpkg and xpkg have only in-package and only external tests.
//   - mainpkg and mainxpkg do the same with package main and main_test.
//   - importer has tests that import the normal package.
//
// The normal package also has a non-test file, and app is a main package that
// does not import normal.
func writeTestOnlyModule(t *testing.T, repoRoot string) string {
	t.Helper()
	pkgDir := filepath.ToSlash(filepath.Join(repoRoot, "pkg"))
	moduleDir := t.TempDir()

	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-testonly

go 1.25.0

require example.com/otelc-testonly/instrumentation v0.0.0

replace example.com/otelc-testonly/instrumentation => ./instrumentation

replace go.opentelemetry.io/otelc/pkg => %s
`, pkgDir))
	writeTestFile(t, moduleDir, "otel.instrumentation.go", testOnlyLicenseHeader+`//go:build tools

package tools

import _ "example.com/otelc-testonly/instrumentation"
`)
	writeTestFile(t, moduleDir, filepath.Join("inpkg", "inpkg_test.go"),
		testOnlyLicenseHeader+"package inpkg\n"+testOnlyHookTest)
	writeTestFile(t, moduleDir, filepath.Join("xpkg", "xpkg_test.go"),
		testOnlyLicenseHeader+"package xpkg_test\n"+testOnlyHookTest)
	writeTestFile(t, moduleDir, filepath.Join("mainpkg", "main_test.go"),
		testOnlyLicenseHeader+"package main\n"+testOnlyHookTest)
	writeTestFile(t, moduleDir, filepath.Join("mainxpkg", "main_test.go"),
		testOnlyLicenseHeader+"package main_test\n"+testOnlyHookTest)
	writeTestFile(t, moduleDir, filepath.Join("normal", "normal.go"), testOnlyLicenseHeader+`package normal

import "example.com/otelc-testonly/instrumentation/lib"

// Do calls lib.Do.
func Do() { lib.Do() }
`)
	writeTestFile(t, moduleDir, filepath.Join("normal", "normal_test.go"),
		testOnlyLicenseHeader+"package normal\n"+testOnlyHookTest)
	writeTestFile(t, moduleDir, filepath.Join("importer", "importer_test.go"), testOnlyLicenseHeader+`package importer

import (
	"os"
	"testing"

	"example.com/otelc-testonly/normal"
)

func TestHook(t *testing.T) {
	normal.Do()
	if os.Getenv("OTELC_TESTONLY_HOOK") != "ran" {
		t.Fatal("the hook on lib.Do did not run")
	}
}
`)
	writeTestFile(t, moduleDir, filepath.Join("app", "main.go"), testOnlyLicenseHeader+`package main

import (
	"fmt"
	"os"

	"example.com/otelc-testonly/instrumentation/lib"
)

func main() {
	lib.Do()
	fmt.Println("hook:", os.Getenv("OTELC_TESTONLY_HOOK"))
}
`)

	// A nested module, so ./... does not select the hook or the target package.
	writeTestFile(t, moduleDir, filepath.Join("instrumentation", "go.mod"), fmt.Sprintf(
		`module example.com/otelc-testonly/instrumentation

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, pkgDir))
	writeTestFile(
		t,
		moduleDir,
		filepath.Join("instrumentation", "hooks.go"),
		testOnlyLicenseHeader+`package instrumentation

import (
	"os"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func Before(ctx hook.HookContext) {
	_ = ctx
	_ = os.Setenv("OTELC_TESTONLY_HOOK", "ran")
}
`,
	)
	writeTestFile(t, moduleDir, filepath.Join("instrumentation", "lib", "lib.go"), testOnlyLicenseHeader+`package lib

func Do() {}
`)
	writeTestFile(t, moduleDir, filepath.Join("instrumentation", "otelc.yaml"), `hook_do:
  target: example.com/otelc-testonly/instrumentation/lib
  where:
    func: Do
  do:
    - inject_hooks:
        before: Before
        path: example.com/otelc-testonly/instrumentation
`)

	return moduleDir
}

// testOnlyOtelcPath returns the absolute path of the otelc binary.
func testOnlyOtelcPath(t *testing.T) string {
	t.Helper()
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)
	return otelcPath
}

// testOnlyRepoRoot returns the absolute path of the repository root.
func testOnlyRepoRoot(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	return repoRoot
}

// requireNoRuntimeFile fails if otelc left a runtime file in a package of
// writeTestOnlyModule that has only _test.go files.
func requireNoRuntimeFile(t *testing.T, moduleDir string) {
	t.Helper()
	for _, pkg := range []string{"inpkg", "xpkg", "mainpkg", "mainxpkg", "importer"} {
		require.NoFileExists(t, filepath.Join(moduleDir, pkg, "otelc.runtime.go"))
	}
}

// TestTestOnlyPackageUsesToolFile runs otelc go test on packages that have
// only _test.go files. The rules of the module's tool file must apply to them.
func TestTestOnlyPackageUsesToolFile(t *testing.T) {
	otelcPath := testOnlyOtelcPath(t)
	repoRoot := testOnlyRepoRoot(t)

	tests := []struct {
		name string
		args []string
		want []string // packages that must pass
	}{
		{name: "in-package tests", args: []string{"./inpkg"}, want: []string{"inpkg"}},
		{name: "external tests", args: []string{"./xpkg"}, want: []string{"xpkg"}},
		{name: "main package tests", args: []string{"./mainpkg"}, want: []string{"mainpkg"}},
		{name: "external main package tests", args: []string{"./mainxpkg"}, want: []string{"mainxpkg"}},
		{name: "with a normal package", args: []string{"./inpkg", "./normal"}, want: []string{"inpkg", "normal"}},
		{
			name: "importing a selected package",
			args: []string{"./importer", "./normal"},
			want: []string{"importer", "normal"},
		},
		{
			name: "all packages",
			args: []string{"./..."},
			want: []string{"inpkg", "xpkg", "mainpkg", "mainxpkg", "importer", "normal"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			moduleDir := writeTestOnlyModule(t, repoRoot)

			args := append([]string{"go", "test", "-count=1"}, tt.args...)
			output := runOtelcCommand(t, moduleDir, os.Environ(), otelcPath, args...)
			require.NotContains(t, output, "no instrumentation will be applied")
			for _, pkg := range tt.want {
				require.Contains(t, output, "ok  \texample.com/otelc-testonly/"+pkg+"\t")
			}
			requireNoRuntimeFile(t, moduleDir)
		})
	}
}

// TestTestOnlyPackageBuild runs otelc go build and otelc go install on a
// module with packages that have only _test.go files. Like go build, they must
// skip those packages.
func TestTestOnlyPackageBuild(t *testing.T) {
	otelcPath := testOnlyOtelcPath(t)
	repoRoot := testOnlyRepoRoot(t)

	t.Run("build all packages", func(t *testing.T) {
		moduleDir := writeTestOnlyModule(t, repoRoot)

		runOtelcCommand(t, moduleDir, os.Environ(), otelcPath, "go", "build", "./...")
		requireNoRuntimeFile(t, moduleDir)
	})

	t.Run("install all packages", func(t *testing.T) {
		moduleDir := writeTestOnlyModule(t, repoRoot)
		gobin := t.TempDir()
		env := append(os.Environ(), "GOBIN="+gobin)

		runOtelcCommand(t, moduleDir, env, otelcPath, "go", "install", "./...")
		requireNoRuntimeFile(t, moduleDir)

		app := filepath.Join(gobin, "app")
		if runtime.GOOS == "windows" {
			app += ".exe"
		}
		out, err := exec.CommandContext(t.Context(), app).CombinedOutput()
		require.NoError(t, err, string(out))
		require.Contains(t, string(out), "hook: ran")
	})
}

// TestTestOnlyPackageBuildError runs otelc go build on a package that has
// only _test.go files. It must fail with the error of go build.
func TestTestOnlyPackageBuildError(t *testing.T) {
	otelcPath := testOnlyOtelcPath(t)
	repoRoot := testOnlyRepoRoot(t)

	for _, pkg := range []string{"inpkg", "xpkg", "mainpkg", "mainxpkg"} {
		t.Run(pkg, func(t *testing.T) {
			moduleDir := writeTestOnlyModule(t, repoRoot)

			goCmd := exec.CommandContext(t.Context(), "go", "build", "./"+pkg)
			goCmd.Dir = moduleDir
			goOut, err := goCmd.CombinedOutput()
			require.Error(t, err, string(goOut))
			goErr := strings.TrimSpace(string(goOut))
			require.NotEmpty(t, goErr)

			cmd := exec.CommandContext(t.Context(), otelcPath, "go", "build", "./"+pkg)
			cmd.Dir = moduleDir
			out, err := cmd.CombinedOutput()
			require.Error(t, err, string(out))
			require.Contains(t, string(out), goErr)
			requireNoRuntimeFile(t, moduleDir)
		})
	}
}
