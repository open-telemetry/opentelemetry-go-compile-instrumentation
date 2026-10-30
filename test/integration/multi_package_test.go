//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

// TestMultiPackageTestBinariesLinkHooks runs `otelc go test` on two packages
// in one invocation. The instrumented lib package references its hook package
// only through //go:linkname, so each test binary must link the hook package.
// The hook imports live in the generated otelc.runtime.go of each test
// target's own package. pkga has no non-test Go files (its only non-test file
// is excluded by a build constraint that is never satisfied, like a CI-tagged
// suite on a foreign platform), so its runtime file must be generated even
// though the package load reports no Go files. The tests do not import the
// hook package, so only the runtime file links it. Each test names its binary
// in the environment before it calls the instrumented dependency, so the hook
// output proves that each binary executed its hook, not just one of the two.
func TestMultiPackageTestBinariesLinkHooks(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-multi-package

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))
	writeTestFile(t, moduleDir, filepath.Join("lib", "lib.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package lib

func Do() int {
	return 1
}
`)
	writeTestFile(t, moduleDir, filepath.Join("hooks", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"fmt"
	"os"

	"go.opentelemetry.io/otelc/pkg/hook"
)

// Before prints the binary name that the test sets in the environment. The
// tests do not import this package: only the generated runtime file of each
// test target links it, so a binary without that file runs no hook.
func Before(ctx hook.HookContext) {
	_ = ctx
	fmt.Println("hook ran:", os.Getenv("OTELC_MULTI_PACKAGE_BINARY"))
}
`)
	// The build constraint is never satisfied, so pkga has no non-test Go
	// files in any build. Its test file still compiles into pkga.test.
	writeTestFile(t, moduleDir, filepath.Join("pkga", "doc.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build otelc_multi_package_never

package pkga
`)
	writeTestFile(t, moduleDir, filepath.Join("pkga", "a_test.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pkga

import (
	"fmt"
	"testing"

	"example.com/otelc-multi-package/lib"
)

func TestA(t *testing.T) {
	t.Setenv("OTELC_MULTI_PACKAGE_BINARY", "pkga")
	if got := lib.Do(); got != 1 {
		t.Fatalf("lib.Do() = %d, want 1", got)
	}
	fmt.Println("pkgA ran")
}
`)
	writeTestFile(t, moduleDir, filepath.Join("pkgb", "b.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pkgb

const Label = "pkgb"
`)
	writeTestFile(t, moduleDir, filepath.Join("pkgb", "b_test.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pkgb

import (
	"fmt"
	"testing"

	"example.com/otelc-multi-package/lib"
)

func TestB(t *testing.T) {
	t.Setenv("OTELC_MULTI_PACKAGE_BINARY", "pkgb")
	if got := lib.Do(); got != 1 {
		t.Fatalf("lib.Do() = %d, want 1", got)
	}
	fmt.Println("pkgB ran")
}
`)
	writeTestFile(t, moduleDir, "rules.yml", `instrument_do:
  target: example.com/otelc-multi-package/lib
  where:
    func: Do
  do:
    - inject_hooks:
        before: Before
        path: example.com/otelc-multi-package/hooks
`)

	env := append(os.Environ(), "OTELC_RULES=rules.yml")
	output := runOtelcCommand(t, moduleDir, env, otelcPath,
		"go", "test", "-count=1", "-v", "./pkga", "./pkgb")
	require.Contains(t, output, "pkgA ran")
	require.Contains(t, output, "pkgB ran")
	require.Contains(t, output, "hook ran: pkga")
	require.Contains(t, output, "hook ran: pkgb")
}

// TestBuildSkipsTestOnlyCommandPackage runs `otelc go build ./...` on a
// module with a matched rule and a directory whose only Go file is a _test.go
// file declaring `package main`. The no-GoFiles fallback is a test-target
// mechanism: a generated non-test file would turn the directory into a
// command without main() and break the wildcard build, so build invocations
// keep the previous skip.
func TestBuildSkipsTestOnlyCommandPackage(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-build-skip

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))
	writeTestFile(t, moduleDir, filepath.Join("lib", "lib.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package lib

func Do() int {
	return 1
}
`)
	writeTestFile(t, moduleDir, filepath.Join("hooks", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import "go.opentelemetry.io/otelc/pkg/hook"

func Before(ctx hook.HookContext) { _ = ctx }
`)
	writeTestFile(t, moduleDir, filepath.Join("cmd", "main_test.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestX(t *testing.T) {}
`)
	writeTestFile(t, moduleDir, "rules.yml", `instrument_do:
  target: example.com/otelc-build-skip/lib
  where:
    func: Do
  do:
    - inject_hooks:
        before: Before
        path: example.com/otelc-build-skip/hooks
`)

	env := append(os.Environ(), "OTELC_RULES=rules.yml")
	runOtelcCommand(t, moduleDir, env, otelcPath, "go", "build", "./...")

	_, err = os.Stat(filepath.Join(moduleDir, "cmd", "otelc.runtime.go"))
	require.True(t, os.IsNotExist(err), "build must not generate a runtime file for a test-only package")
}
