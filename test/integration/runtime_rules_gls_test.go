//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
	"go.opentelemetry.io/otelc/tool/util"
)

// TestIgnoreDirectiveWithRuntimeRules verifies that a rule of the application
// that targets the runtime package does not hide otelc's own runtime rules.
// Statement level //otelc:ignore calls the suppression functions that those
// rules add to runtime, so the build fails when they are missing.
func TestIgnoreDirectiveWithRuntimeRules(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	appDir := t.TempDir()
	writeTestFile(t, appDir, "go.mod", fmt.Sprintf(`module example.com/app

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))

	writeTestFile(t, appDir, "hooks/hooks.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import "go.opentelemetry.io/otelc/pkg/hook"

func Before(_ hook.HookContext) { println("hook") }
`)

	writeTestFile(t, appDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

func work() { println("work") }

func main() {
	work()
	//otelc:ignore
	work()
}
`)

	writeTestFile(t, appDir, "rules.yml", `instrument_work:
  target: main
  where:
    func: work
  do:
    - inject_hooks:
        before: Before
        path: example.com/app/hooks
runtime_field:
  target: runtime
  where:
    struct: g
  do:
    - add_struct_fields:
        new_field:
          - name: otelcTestMarker
            type: bool
`)

	outputPath := filepath.Join(appDir, "app_bin")
	if util.IsWindows() {
		outputPath += ".exe"
	}
	env := append(os.Environ(), "OTELC_RULES="+filepath.Join(appDir, "rules.yml"))
	runOtelcCommand(t, appDir, env, otelcPath, "go", "build", "-o", outputPath, ".")

	runCmd := exec.CommandContext(t.Context(), outputPath)
	runCmd.Dir = appDir
	out, err := runCmd.CombinedOutput()
	require.NoError(t, err, "running built binary failed: %s", string(out))
	// The hook runs for the first call only. The ignored call still runs work.
	require.Equal(t, "hook\nwork\nwork\n", string(out))
}
