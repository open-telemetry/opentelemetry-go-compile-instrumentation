// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration && cgo

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

func TestCgoPackagePassesVet(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-cgo-vet

go 1.25.0

require example.com/otelc-cgo-vet/instrumentation v0.0.0

replace example.com/otelc-cgo-vet/instrumentation => ./instrumentation

replace go.opentelemetry.io/otelc/pkg => %s
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))
	writeTestFile(t, moduleDir, "otel.instrumentation.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build tools

package tools

import _ "example.com/otelc-cgo-vet/instrumentation"
`)
	writeTestFile(t, moduleDir, "cgo.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package cgovet

/*
static int answer(void) { return 42; }
*/
import "C"

func Answer() int {
	return int(C.answer())
}
`)
	writeTestFile(t, moduleDir, "cgo_test.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package cgovet

import "testing"

func TestAnswer(t *testing.T) {
	if got := Answer(); got != 42 {
		t.Fatalf("Answer() = %d, want 42", got)
	}
}
`)
	writeTestFile(
		t,
		moduleDir,
		filepath.Join("instrumentation", "go.mod"),
		fmt.Sprintf(`module example.com/otelc-cgo-vet/instrumentation

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))),
	)
	writeTestFile(t, moduleDir, filepath.Join("instrumentation", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrumentation

import "go.opentelemetry.io/otelc/pkg/hook"

func Before(ctx hook.HookContext) {
	_ = ctx
}
`)
	writeTestFile(t, moduleDir, filepath.Join("instrumentation", "otelc.yaml"), `instrument_cgo:
  target: example.com/otelc-cgo-vet
  where:
    func: Answer
  do:
    - inject_hooks:
        before: Before
        path: example.com/otelc-cgo-vet/instrumentation
`)

	env := os.Environ()
	runOtelcCommand(t, moduleDir, env, otelcPath, "setup", ".")
	runOtelcCommand(t, moduleDir, env, otelcPath, "go", "test", "-count=1", "./...")
}

func writeTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func runOtelcCommand(t *testing.T, dir string, env []string, otelcPath string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), otelcPath, args...)
	cmd.Dir = dir
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s failed:\n%s", cmd.String(), output)
}
