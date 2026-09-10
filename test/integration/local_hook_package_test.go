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

func TestLocalHookPackageDoesNotImportItself(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-self-import

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))
	writeTestFile(t, moduleDir, "answer.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package selfimport

func Answer() int {
	return 42
}
`)
	writeTestFile(t, moduleDir, "answer_test.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package selfimport

import "testing"

func TestAnswer(t *testing.T) {
	if got := Answer(); got != 42 {
		t.Fatalf("Answer() = %d, want 42", got)
	}
}
`)
	writeTestFile(t, moduleDir, filepath.Join("hooks", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"fmt"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func Before(ctx hook.HookContext) {
	_ = ctx
	fmt.Println("local hook ran")
}
`)
	writeTestFile(t, moduleDir, "rules.yml", `instrument_answer:
  target: example.com/otelc-self-import
  where:
    func: Answer
  do:
    - inject_hooks:
        before: Before
        path: example.com/otelc-self-import/hooks
`)

	env := append(os.Environ(), "OTELC_RULES=rules.yml")
	output := runOtelcCommand(t, moduleDir, env, otelcPath, "go", "test", "-count=1", "-v", "./...")
	require.Contains(t, output, "local hook ran")
}
