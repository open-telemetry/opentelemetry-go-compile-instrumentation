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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
	"go.opentelemetry.io/otelc/tool/util"
)

// TestFileRuleSourceImports reproduces and verifies that dependencies introduced
// solely by an add_file rule's source file (guarded by //go:build ignore) are
// discovered during setup by matchInjectedDeps so that rules targeting those
// injected packages are matched and instrumented in the build.
func TestFileRuleSourceImports(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	appDir := t.TempDir()

	writeTestFile(t, appDir, filepath.Join("extra", "go.mod"), `module example.com/extra

go 1.25.0
`)
	writeTestFile(t, appDir, filepath.Join("extra", "extra.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package extra

var message = "loaded-from-extra-module"

func Message() string {
	return message
}
`)

	writeTestFile(t, appDir, filepath.Join("hooks", "doc.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks
`)
	writeTestFile(t, appDir, filepath.Join("hooks", "added.go"), `//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"fmt"
	"example.com/extra"
)

func init() {
	fmt.Println("init injected:", extra.Message())
}
`)

	writeTestFile(t, appDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

func main() {
	fmt.Println("main executed successfully")
}
`)

	writeTestFile(t, appDir, "main_test.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestMainExecution(t *testing.T) {
	main()
}
`)

	writeTestFile(t, appDir, "go.mod", fmt.Sprintf(`module example.com/app

go 1.25.0

require (
	example.com/extra v0.0.0
	go.opentelemetry.io/otelc/pkg v0.0.0
)

replace (
	example.com/extra => ./extra
	go.opentelemetry.io/otelc/pkg => %s
)
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))

	writeTestFile(t, appDir, "rules.yml", `inject_added_file:
  target: main
  do:
    - add_file:
        file: added.go
        path: example.com/app/hooks

instrument_injected_dep:
  target: example.com/extra
  where:
    func: Message
  do:
    - inject_code:
        raw: 'message = "instrumented-from-extra-module"'
`)

	goCache := filepath.Join(t.TempDir(), "gocache")
	binary := filepath.Join(appDir, "app")
	if util.IsWindows() {
		binary += ".exe"
	}

	env := append(
		os.Environ(),
		"GOWORK=off",
		"GOCACHE="+goCache,
		"OTELC_RULES=rules.yml",
	)

	t.Run("cold build and execution", func(t *testing.T) {
		cmd := exec.CommandContext(t.Context(), otelcPath, "go", "build", "-o", binary, ".")
		cmd.Dir = appDir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "cold build failed:\n%s", out)

		runCmd := exec.CommandContext(t.Context(), binary)
		runCmd.Dir = appDir
		runOut, err := runCmd.CombinedOutput()
		require.NoError(t, err, "running instrumented binary failed:\n%s", runOut)

		outputStr := string(runOut)
		assert.Contains(t, outputStr, "init injected: instrumented-from-extra-module")
		assert.Contains(t, outputStr, "main executed successfully")
	})

	t.Run("repeated warm build", func(t *testing.T) {
		cmd := exec.CommandContext(t.Context(), otelcPath, "go", "build", "-o", binary, ".")
		cmd.Dir = appDir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "repeated warm build failed:\n%s", out)
	})

	t.Run("go test execution", func(t *testing.T) {
		cmd := exec.CommandContext(t.Context(), otelcPath, "go", "test", "-v", "-count=1", ".")
		cmd.Dir = appDir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "go test failed:\n%s", out)
		assert.Contains(t, string(out), "init injected: instrumented-from-extra-module")
	})
}
