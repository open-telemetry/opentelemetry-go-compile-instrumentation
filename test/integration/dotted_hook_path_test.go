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

// TestHookPackageWithDotInLastPathElement builds against a hook package whose
// import path ends in a dotted element (".../hooks.v2", the same shape as
// github.com/nats-io/nats.go). The Go toolchain emits that package's symbols
// under an escaped prefix (".../hooks%2ev2"), so every //go:linkname target
// otelc generates for it, the hook trampoline in the instrumented package and
// the stack-helper vars in otelc.runtime.go, must use the escaped form or the
// link fails with "relocation target ... not defined".
func TestHookPackageWithDotInLastPathElement(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-dotted

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))
	writeTestFile(t, moduleDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

func Answer() int {
	return 42
}

func main() {
	fmt.Println("answer", Answer())
}
`)
	writeTestFile(t, moduleDir, filepath.Join("hooks.v2", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"fmt"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func BeforeAnswer(ctx hook.HookContext) {
	_ = ctx
	fmt.Println("dotted hook ran")
}
`)
	writeTestFile(t, moduleDir, "rules.yml", `hook_answer:
  target: main
  where:
    func: Answer
  do:
    - inject_hooks:
        before: BeforeAnswer
        path: example.com/otelc-dotted/hooks.v2
`)

	env := append(os.Environ(), "OTELC_RULES=rules.yml")
	bin := filepath.Join(t.TempDir(), "app")
	if util.IsWindows() {
		bin += ".exe"
	}
	runOtelcCommand(t, moduleDir, env, otelcPath, "go", "build", "-o", bin, ".")

	out, err := exec.CommandContext(t.Context(), bin).CombinedOutput()
	require.NoError(t, err, "running %s:\n%s", bin, out)
	require.Contains(t, string(out), "dotted hook ran")
	require.Contains(t, string(out), "answer 42")
}
