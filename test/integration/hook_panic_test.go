// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

// TestHookPanicRecovery verifies that deliberate panics inside Before and After hooks
// are recovered and isolated by the trampoline:
// 1. The panic is recovered and does not crash the host process (exit code 0).
// 2. Failure log messages and panic error text are printed to output.
// 3. Instrumented target functions continue execution and return expected results.
// 4. Unit tests pass under `otelc go test ./...` despite panicking hooks.
func TestHookPanicRecovery(t *testing.T) {
	otelcPath, pkgReplacement := getTestEnvPaths(t)
	moduleDir := t.TempDir()

	writeMultiPkgTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-panic-test

go 1.25.0

require (
	example.com/otelc-panic-test/hooks v0.0.0
	go.opentelemetry.io/otelc/pkg v0.0.0
)

replace example.com/otelc-panic-test/hooks => ./hooks

replace go.opentelemetry.io/otelc/pkg => %s
`, pkgReplacement))

	// main package invoking instrumented work functions
	writeMultiPkgTestFile(t, moduleDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"example.com/otelc-panic-test/work"
)

func main() {
	resB := work.TargetBefore()
	fmt.Println("BeforeResult:", resB)

	resA := work.TargetAfter()
	fmt.Println("AfterResult:", resA)

	fmt.Println("Execution finished successfully")
}
`)

	// work package containing the instrumented target functions
	writeMultiPkgTestFile(t, moduleDir, filepath.Join("work", "work.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package work

func TargetBefore() string {
	return "target-before-result"
}

func TargetAfter() string {
	return "target-after-result"
}
`)

	writeMultiPkgTestFile(t, moduleDir, filepath.Join("work", "work_test.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package work

import "testing"

func TestTargetBefore(t *testing.T) {
	got := TargetBefore()
	if got != "target-before-result" {
		t.Fatalf("TargetBefore() = %q, want target-before-result", got)
	}
}

func TestTargetAfter(t *testing.T) {
	got := TargetAfter()
	if got != "target-after-result" {
		t.Fatalf("TargetAfter() = %q, want target-after-result", got)
	}
}
`)

	writeMultiPkgTestFile(
		t,
		moduleDir,
		filepath.Join("hooks", "go.mod"),
		fmt.Sprintf(`module example.com/otelc-panic-test/hooks

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, pkgReplacement),
	)

	writeMultiPkgTestFile(t, moduleDir, filepath.Join("hooks", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"errors"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func PanickingBefore(ctx hook.HookContext) {
	_ = ctx
	panic(errors.New("deliberate before hook panic"))
}

func PanickingAfter(ctx hook.HookContext, ret string) {
	_ = ctx
	_ = ret
	panic(errors.New("deliberate after hook panic"))
}
`)

	writeMultiPkgTestFile(t, moduleDir, "rules.yml", `instrument_target_before:
  target: example.com/otelc-panic-test/work
  where:
    func: TargetBefore
  do:
    - inject_hooks:
        before: PanickingBefore
        path: example.com/otelc-panic-test/hooks

instrument_target_after:
  target: example.com/otelc-panic-test/work
  where:
    func: TargetAfter
  do:
    - inject_hooks:
        after: PanickingAfter
        path: example.com/otelc-panic-test/hooks
`)

	env := append(os.Environ(), util.EnvOtelcRules+"=rules.yml")

	// 1. Verify `otelc go test ./...` passes despite panicking hooks
	runMultiPkgOtelcCommand(t, moduleDir, env, otelcPath, "go", "test", "-count=1", "-v", "./...")

	// 2. Build the binary using `otelc go build`
	binPath := filepath.Join(moduleDir, "app")
	runMultiPkgOtelcCommand(t, moduleDir, env, otelcPath, "go", "build", "-o", binPath, ".")

	// 3. Execute the binary and inspect output
	cmd := exec.CommandContext(t.Context(), binPath)
	outputBytes, err := cmd.CombinedOutput()
	output := string(outputBytes)

	// Process must exit cleanly without crashing from unhandled panic
	require.NoError(t, err, "process exited with error:\n%s", output)

	// Target function continued and executed
	assert.Contains(t, output, "BeforeResult: target-before-result")
	assert.Contains(t, output, "AfterResult: target-after-result")
	assert.Contains(t, output, "Execution finished successfully")

	// Verify panic recovery logs and error messages are emitted
	assert.Contains(t, output, "failed to exec Before hook PanickingBefore")
	assert.Contains(t, output, "deliberate before hook panic")
	assert.Contains(t, output, "failed to exec After hook PanickingAfter")
	assert.Contains(t, output, "deliberate after hook panic")
}
