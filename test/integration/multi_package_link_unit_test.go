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

	"go.opentelemetry.io/otelc/test/testutil"
	"go.opentelemetry.io/otelc/tool/util"
)

// TestMultiPackageLinkUnit_BothHaveTests verifies that when a root package imports
// a dependency, and BOTH the root package and dependency have unit tests running
// under `otelc go test ./...`:
// 1. Neither test binary suffers from duplicate-symbol linker errors (Issue #1361).
// 2. Instrumented functions in both packages execute correctly.
func TestMultiPackageLinkUnit_BothHaveTests(t *testing.T) {
	otelcPath, pkgReplacement := getTestEnvPaths(t)
	moduleDir := t.TempDir()

	// Root application module
	writeMultiPkgTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-multi-pkg

go 1.25.0

require (
	example.com/otelc-multi-pkg/hooks v0.0.0
	go.opentelemetry.io/otelc/pkg v0.0.0
)

replace example.com/otelc-multi-pkg/hooks => ./hooks

replace go.opentelemetry.io/otelc/pkg => %s
`, pkgReplacement))

	// Root package app importing subpackage lib
	writeMultiPkgTestFile(t, moduleDir, "answer.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"example.com/otelc-multi-pkg/hooks"
	"example.com/otelc-multi-pkg/lib"
)

func Answer() int {
	_ = lib.Helper()
	return 42 + hooks.AnswerHookCounter
}
`)

	// Root package unit test
	writeMultiPkgTestFile(t, moduleDir, "answer_test.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"testing"

	"example.com/otelc-multi-pkg/hooks"
)

func TestAnswer(t *testing.T) {
	got := Answer()
	if got != 43 {
		t.Fatalf("Answer() = %d, want 43", got)
	}
	if hooks.AnswerHookCounter < 1 {
		t.Fatalf("hooks.AnswerHookCounter = %d, want >= 1", hooks.AnswerHookCounter)
	}
}
`)

	// Subpackage lib
	writeMultiPkgTestFile(t, moduleDir, filepath.Join("lib", "lib.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package lib

import "example.com/otelc-multi-pkg/hooks"

func Helper() string {
	return "helper-" + hooks.LibHookText
}
`)

	// Subpackage lib unit test (runs in its own isolated test binary during go test ./...)
	writeMultiPkgTestFile(t, moduleDir, filepath.Join("lib", "lib_test.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package lib

import (
	"testing"

	"example.com/otelc-multi-pkg/hooks"
)

func TestHelper(t *testing.T) {
	got := Helper()
	if got != "helper-instrumented" {
		t.Fatalf("Helper() = %q, want helper-instrumented", got)
	}
	if hooks.LibHookCounter < 1 {
		t.Fatalf("hooks.LibHookCounter = %d, want >= 1", hooks.LibHookCounter)
	}
}
`)

	// External hook module with state counters to verify execution
	writeMultiPkgTestFile(
		t,
		moduleDir,
		filepath.Join("hooks", "go.mod"),
		fmt.Sprintf(`module example.com/otelc-multi-pkg/hooks

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, pkgReplacement),
	)

	writeMultiPkgTestFile(t, moduleDir, filepath.Join("hooks", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import "go.opentelemetry.io/otelc/pkg/hook"

var (
	AnswerHookCounter int
	LibHookCounter    int
	LibHookText       = "uninstrumented"
)

func BeforeAnswer(ctx hook.HookContext) {
	_ = ctx
	AnswerHookCounter++
}

func BeforeLib(ctx hook.HookContext) {
	_ = ctx
	LibHookCounter++
	LibHookText = "instrumented"
}
`)

	// Rules targeting both Answer and Helper
	writeMultiPkgTestFile(t, moduleDir, "rules.yml", `instrument_answer:
  target: example.com/otelc-multi-pkg
  where:
    func: Answer
  do:
    - inject_hooks:
        before: BeforeAnswer
        path: example.com/otelc-multi-pkg/hooks

instrument_helper:
  target: example.com/otelc-multi-pkg/lib
  where:
    func: Helper
  do:
    - inject_hooks:
        before: BeforeLib
        path: example.com/otelc-multi-pkg/hooks
`)

	env := append(os.Environ(), util.EnvOtelcRules+"=rules.yml")

	// 1. otelc go test ./... must pass both test packages without linker collisions
	runMultiPkgOtelcCommand(t, moduleDir, env, otelcPath, "go", "test", "-count=1", "-v", "./...")

	// 2. otelc go build ./... must build cleanly
	runMultiPkgOtelcCommand(t, moduleDir, env, otelcPath, "go", "build", "./...")

	// 3. otelc setup ./... generates runtime files to inspect linkname placement
	runMultiPkgOtelcCommand(t, moduleDir, env, otelcPath, "setup", "./...")

	// The root package receives hook imports and no linknames
	rootRuntimeFile := filepath.Join(moduleDir, "otelc.runtime.go")
	require.FileExists(t, rootRuntimeFile)
	rootContent, err := os.ReadFile(rootRuntimeFile)
	require.NoError(t, err)
	assert.NotContains(t, string(rootContent), `//go:linkname`)
	assert.NotContains(t, string(rootContent), `_getstack`)
	assert.NotContains(t, string(rootContent), `_printstack`)
	assert.NotContains(t, string(rootContent), `runtime/debug`)
	assert.NotContains(t, string(rootContent), `"log"`)
	assert.Contains(t, string(rootContent), `_ "example.com/otelc-multi-pkg/hooks"`)

	// The subpackage lib in the same link unit receives hook imports but NOT linknames
	libRuntimeFile := filepath.Join(moduleDir, "lib", "otelc.runtime.go")
	require.FileExists(t, libRuntimeFile)
	libContent, err := os.ReadFile(libRuntimeFile)
	require.NoError(t, err)
	assert.NotContains(t, string(libContent), `//go:linkname`)
	assert.NotContains(t, string(libContent), `_getstack`)
	assert.NotContains(t, string(libContent), `_printstack`)
	assert.NotContains(t, string(libContent), `runtime/debug`)
	assert.NotContains(t, string(libContent), `"log"`)
	assert.Contains(t, string(libContent), `_ "example.com/otelc-multi-pkg/hooks"`)
}

// TestMultiPackageLinkUnit_MultipleMainPackages verifies that when multiple main
// packages (cmd/api, cmd/worker) import a shared library:
// 1. `otelc go build ./...` produces no duplicate linkname definitions.
// 2. Both binaries are generated and execute successfully.
// 3. Packages emit blank hook imports without linknames.
func TestMultiPackageLinkUnit_MultipleMainPackages(t *testing.T) {
	otelcPath, pkgReplacement := getTestEnvPaths(t)
	moduleDir := t.TempDir()

	writeMultiPkgTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-multi-main

go 1.25.0

require (
	example.com/otelc-multi-main/hooks v0.0.0
	go.opentelemetry.io/otelc/pkg v0.0.0
)

replace example.com/otelc-multi-main/hooks => ./hooks

replace go.opentelemetry.io/otelc/pkg => %s
`, pkgReplacement))

	// Shared library
	writeMultiPkgTestFile(t, moduleDir, filepath.Join("shared", "shared.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package shared

func Compute() string {
	return "shared-result"
}
`)

	// Main binary 1: cmd/api
	writeMultiPkgTestFile(t, moduleDir, filepath.Join("cmd", "api", "main.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"example.com/otelc-multi-main/shared"
)

func ApiWork() string {
	return "api:" + shared.Compute()
}

func main() {
	fmt.Println(ApiWork())
}
`)

	// Main binary 2: cmd/worker
	writeMultiPkgTestFile(
		t,
		moduleDir,
		filepath.Join("cmd", "worker", "main.go"),
		`// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"example.com/otelc-multi-main/shared"
)

func WorkerWork() string {
	return "worker:" + shared.Compute()
}

func main() {
	fmt.Println(WorkerWork())
}
`)

	// Hook package
	writeMultiPkgTestFile(
		t,
		moduleDir,
		filepath.Join("hooks", "go.mod"),
		fmt.Sprintf(`module example.com/otelc-multi-main/hooks

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, pkgReplacement),
	)

	writeMultiPkgTestFile(t, moduleDir, filepath.Join("hooks", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import "go.opentelemetry.io/otelc/pkg/hook"

func Before(ctx hook.HookContext) {
	_ = ctx
}
`)

	// Rule targeting shared.Compute
	writeMultiPkgTestFile(t, moduleDir, "rules.yml", `instrument_shared:
  target: example.com/otelc-multi-main/shared
  where:
    func: Compute
  do:
    - inject_hooks:
        before: Before
        path: example.com/otelc-multi-main/hooks
`)

	env := append(os.Environ(), util.EnvOtelcRules+"=rules.yml")

	// 1. Build all binaries via otelc go build ./cmd/... (verifies multi-package build succeeds)
	runMultiPkgOtelcCommand(t, moduleDir, env, otelcPath, "go", "build", "./cmd/...")

	// Build individual binaries with -o to execute and verify their output
	apiBin := filepath.Join(moduleDir, "api")
	workerBin := filepath.Join(moduleDir, "worker")
	runMultiPkgOtelcCommand(t, moduleDir, env, otelcPath, "go", "build", "-o", apiBin, "./cmd/api")
	runMultiPkgOtelcCommand(t, moduleDir, env, otelcPath, "go", "build", "-o", workerBin, "./cmd/worker")

	apiOut, err := exec.CommandContext(t.Context(), apiBin).CombinedOutput()
	require.NoError(t, err, "running api binary: %s", apiOut)
	assert.Contains(t, string(apiOut), "api:shared-result")

	workerOut, err := exec.CommandContext(t.Context(), workerBin).CombinedOutput()
	require.NoError(t, err, "running worker binary: %s", workerOut)
	assert.Contains(t, string(workerOut), "worker:shared-result")

	// Run otelc setup ./... to inspect generated runtime files
	runMultiPkgOtelcCommand(t, moduleDir, env, otelcPath, "setup", "./cmd/...", "./shared")

	// cmd/api receives hook imports and no linknames
	apiRuntime := filepath.Join(moduleDir, "cmd", "api", "otelc.runtime.go")
	require.FileExists(t, apiRuntime)
	apiContent, err := os.ReadFile(apiRuntime)
	require.NoError(t, err)
	assert.NotContains(t, string(apiContent), `//go:linkname`)
	assert.Contains(t, string(apiContent), `_ "example.com/otelc-multi-main/hooks"`)

	// cmd/worker receives hook imports and no linknames
	workerRuntime := filepath.Join(moduleDir, "cmd", "worker", "otelc.runtime.go")
	require.FileExists(t, workerRuntime)
	workerContent, err := os.ReadFile(workerRuntime)
	require.NoError(t, err)
	assert.NotContains(t, string(workerContent), `//go:linkname`)
	assert.Contains(t, string(workerContent), `_ "example.com/otelc-multi-main/hooks"`)

	// shared is imported by both: also receives hook imports and no linknames
	sharedRuntime := filepath.Join(moduleDir, "shared", "otelc.runtime.go")
	require.FileExists(t, sharedRuntime)
	sharedContent, err := os.ReadFile(sharedRuntime)
	require.NoError(t, err)
	assert.NotContains(t, string(sharedContent), `//go:linkname`)
	assert.Contains(t, string(sharedContent), `_ "example.com/otelc-multi-main/hooks"`)
}

// TestMultiPackageLinkUnit_RuleTargetsOnlyDependency verifies that when a rule
// targets ONLY a dependency package (`lib`) and does not target the root package (`app`):
// 1. The dependency is instrumented correctly.
// 2. The root package builds and links cleanly with the hook linknames.
func TestMultiPackageLinkUnit_RuleTargetsOnlyDependency(t *testing.T) {
	otelcPath, pkgReplacement := getTestEnvPaths(t)
	moduleDir := t.TempDir()

	writeMultiPkgTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-only-dep

go 1.25.0

require (
	example.com/otelc-only-dep/hooks v0.0.0
	go.opentelemetry.io/otelc/pkg v0.0.0
)

replace example.com/otelc-only-dep/hooks => ./hooks

replace go.opentelemetry.io/otelc/pkg => %s
`, pkgReplacement))

	// Root package app (uninstrumented)
	writeMultiPkgTestFile(t, moduleDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"example.com/otelc-only-dep/lib"
)

func main() {
	fmt.Println("Result:", lib.Work())
}
`)

	// Dependency package lib (targeted by rule)
	writeMultiPkgTestFile(t, moduleDir, filepath.Join("lib", "lib.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package lib

import "example.com/otelc-only-dep/hooks"

func Work() string {
	return "work-" + hooks.Status
}
`)

	writeMultiPkgTestFile(t, moduleDir, filepath.Join("lib", "lib_test.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package lib

import (
	"testing"
	"example.com/otelc-only-dep/hooks"
)

func TestWork(t *testing.T) {
	res := Work()
	if res != "work-hooked" {
		t.Fatalf("Work() = %q, want work-hooked", res)
	}
	if hooks.Status != "hooked" {
		t.Fatalf("hooks.Status = %q, want hooked", hooks.Status)
	}
}
`)

	// Hook module
	writeMultiPkgTestFile(
		t,
		moduleDir,
		filepath.Join("hooks", "go.mod"),
		fmt.Sprintf(`module example.com/otelc-only-dep/hooks

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, pkgReplacement),
	)

	writeMultiPkgTestFile(t, moduleDir, filepath.Join("hooks", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import "go.opentelemetry.io/otelc/pkg/hook"

var Status = "unhooked"

func Before(ctx hook.HookContext) {
	_ = ctx
	Status = "hooked"
}
`)

	// Rule targets ONLY lib.Work
	writeMultiPkgTestFile(t, moduleDir, "rules.yml", `instrument_dep_only:
  target: example.com/otelc-only-dep/lib
  where:
    func: Work
  do:
    - inject_hooks:
        before: Before
        path: example.com/otelc-only-dep/hooks
`)

	env := append(os.Environ(), util.EnvOtelcRules+"=rules.yml")

	// 1. otelc go test ./... must pass
	runMultiPkgOtelcCommand(t, moduleDir, env, otelcPath, "go", "test", "-count=1", "-v", "./...")

	// 2. otelc go build . must succeed
	runMultiPkgOtelcCommand(t, moduleDir, env, otelcPath, "go", "build", ".")
}

func getTestEnvPaths(t *testing.T) (otelcPath, pkgReplacement string) {
	t.Helper()
	var err error
	otelcPath, err = testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)
	otelcPath = safeOtelcBinary(t, otelcPath)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	pkgReplacement = safePkgReplacement(t, repoRoot)
	return otelcPath, pkgReplacement
}

func writeMultiPkgTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func runMultiPkgOtelcCommand(t *testing.T, dir string, env []string, otelcPath string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), otelcPath, args...)
	cmd.Dir = dir
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s failed:\n%s", cmd.String(), output)
}

func safePkgReplacement(t *testing.T, repoRoot string) string {
	t.Helper()
	pkgPath := filepath.Join(repoRoot, "pkg")
	if !strings.Contains(pkgPath, " ") {
		return filepath.ToSlash(pkgPath)
	}
	linkDir := filepath.Join(t.TempDir(), "pkg")
	if err := os.Symlink(pkgPath, linkDir); err == nil {
		return filepath.ToSlash(linkDir)
	}
	return filepath.ToSlash(pkgPath)
}

func safeOtelcBinary(t *testing.T, otelcPath string) string {
	t.Helper()
	if !strings.Contains(otelcPath, " ") {
		return otelcPath
	}
	tmpBin := filepath.Join(t.TempDir(), "otelc")
	data, err := os.ReadFile(otelcPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tmpBin, data, 0o755))
	return tmpBin
}
