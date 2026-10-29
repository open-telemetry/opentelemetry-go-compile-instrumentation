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
)

// TestMultiPackageLinkUnit selects several packages that end up in one binary.
// Each selected package receives an otelc.runtime.go, and the link must not
// fail with a duplicated definition of the hook stack symbols.
func TestMultiPackageLinkUnit(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-multi-pkg

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))
	writeTestFile(t, moduleDir, "answer.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package app

import "example.com/otelc-multi-pkg/lib"

func Answer() int {
	_ = lib.Helper()
	return 42
}
`)
	writeTestFile(t, moduleDir, "answer_test.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package app

import "testing"

func TestAnswer(t *testing.T) {
	if got := Answer(); got != 42 {
		t.Fatalf("Answer() = %d, want 42", got)
	}
}
`)
	writeTestFile(t, moduleDir, filepath.Join("lib", "lib.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package lib

func Helper() string { return "helper" }
`)
	writeTestFile(t, moduleDir, filepath.Join("lib", "lib_test.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package lib

import "testing"

func TestHelper(t *testing.T) {
	if got := Helper(); got != "helper" {
		t.Fatalf("Helper() = %q, want helper", got)
	}
}
`)
	for _, name := range []string{"one", "two"} {
		writeTestFile(t, moduleDir, filepath.Join("cmd", name, "main.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	app "example.com/otelc-multi-pkg"
	"example.com/otelc-multi-pkg/lib"
)

func main() {
	fmt.Println(app.Answer(), lib.Helper())
}
`)
	}
	writeTestFile(t, moduleDir, filepath.Join("hooks", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"fmt"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func Before(ctx hook.HookContext) {
	_ = ctx
	fmt.Println("multi-package hook ran")
}
`)
	writeTestFile(t, moduleDir, "rules.yml", `instrument_answer:
  target: example.com/otelc-multi-pkg
  where:
    func: Answer
  do:
    - inject_hooks:
        before: Before
        path: example.com/otelc-multi-pkg/hooks
`)

	env := append(os.Environ(), "OTELC_RULES=rules.yml")

	t.Run("test", func(t *testing.T) {
		output := runOtelcCommand(t, moduleDir, env, otelcPath, "go", "test", "-count=1", "-v", "./...")
		require.Contains(t, output, "multi-package hook ran")
	})

	t.Run("build", func(t *testing.T) {
		binDir := t.TempDir()
		runOtelcCommand(t, moduleDir, env, otelcPath, "go", "build", "-o", binDir+string(filepath.Separator), "./...")
		for _, name := range []string{"one", "two"} {
			bin := filepath.Join(binDir, name)
			if _, statErr := os.Stat(bin + ".exe"); statErr == nil {
				bin += ".exe"
			}
			output, runErr := exec.CommandContext(t.Context(), bin).CombinedOutput()
			require.NoError(t, runErr, "%s failed:\n%s", bin, output)
			require.Contains(t, string(output), "multi-package hook ran")
		}
	})
}
