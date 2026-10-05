// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration && cgo

package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

// TestGlobTargetCallRuleSkipsUnchangedFiles builds with a call rule whose "**"
// target also reaches runtime/cgo, which only the hook package brings into the
// build. otelc has no cgo file mapping for such a package, so rewriting its
// files fails the build. No call in it matches, so it must stay unchanged.
func TestGlobTargetCallRuleSkipsUnchangedFiles(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-glob

go 1.25.0

require example.com/otelc-glob/instrumentation v0.0.0

replace example.com/otelc-glob/instrumentation => ./instrumentation

replace go.opentelemetry.io/otelc/pkg => %s
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))
	writeTestFile(t, moduleDir, "otel.instrumentation.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build tools

package tools

import _ "example.com/otelc-glob/instrumentation"
`)
	writeTestFile(t, moduleDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

func Answer() int {
	return 42
}

func main() {
	fmt.Println(Answer())
}
`)
	writeTestFile(
		t,
		moduleDir,
		filepath.Join("instrumentation", "go.mod"),
		fmt.Sprintf(`module example.com/otelc-glob/instrumentation

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))),
	)
	writeTestFile(t, moduleDir, filepath.Join("instrumentation", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrumentation

import (
	_ "runtime/cgo"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func Before(ctx hook.HookContext) {
	_ = ctx
}
`)
	writeTestFile(t, moduleDir, filepath.Join("instrumentation", "otelc.yaml"), `hook_answer:
  target: main
  where:
    func: Answer
  do:
    - inject_hooks:
        before: Before
        path: example.com/otelc-glob/instrumentation
wrap_http_get:
  target: "**"
  where:
    function_call: net/http.Get
  do:
    - wrap_call:
        replace: "{{ . }}"
`)

	// A fresh build cache makes the build compile runtime/cgo instead of
	// reusing an archive from an earlier build.
	env := append(os.Environ(), "GOCACHE="+t.TempDir())
	bin := filepath.Join(t.TempDir(), "app")
	runOtelcCommand(t, moduleDir, env, otelcPath, "go", "build", "-o", bin, ".")

	out, err := exec.CommandContext(t.Context(), bin).CombinedOutput()
	require.NoError(t, err, "running %s:\n%s", bin, out)
	require.Equal(t, "42", strings.TrimSpace(string(out)))
}
