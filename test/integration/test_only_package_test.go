// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"os"
	"path/filepath"
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
// The packages inpkg and xpkg have only in-package and only external
// _test.go files, and normal also has a non-test file.
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
	writeTestFile(t, moduleDir, filepath.Join("normal", "normal.go"), testOnlyLicenseHeader+"package normal\n")
	writeTestFile(t, moduleDir, filepath.Join("normal", "normal_test.go"),
		testOnlyLicenseHeader+"package normal\n"+testOnlyHookTest)

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

// TestTestOnlyPackageUsesToolFile runs otelc go test on packages that have
// only _test.go files. The rules of the module's tool file must apply to them.
func TestTestOnlyPackageUsesToolFile(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	tests := []struct {
		name string
		args []string
		want []string // packages that must pass
	}{
		{name: "in-package tests", args: []string{"./inpkg"}, want: []string{"inpkg"}},
		{name: "external tests", args: []string{"./xpkg"}, want: []string{"xpkg"}},
		{name: "with a normal package", args: []string{"./inpkg", "./normal"}, want: []string{"inpkg", "normal"}},
		{name: "all packages", args: []string{"./..."}, want: []string{"inpkg", "xpkg", "normal"}},
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
		})
	}
}
