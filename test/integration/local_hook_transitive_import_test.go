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

func TestLocalHookPackageImportingSiblingDoesNotCycle(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-transitive-import

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))
	writeTestFile(t, moduleDir, "answer.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package transitiveimport

func Answer() int {
	return 42
}
`)
	writeTestFile(t, moduleDir, "answer_test.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package transitiveimport

import "testing"

func TestAnswer(t *testing.T) {
	if got := Answer(); got != 42 {
		t.Fatalf("Answer() = %d, want 42", got)
	}
}
`)
	writeTestFile(t, moduleDir, filepath.Join("internal", "log", "log.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import "fmt"

func Print(s string) {
	fmt.Println(s)
}
`)
	writeTestFile(t, moduleDir, filepath.Join("hooks", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"example.com/otelc-transitive-import/internal/log"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func Before(ctx hook.HookContext) {
	_ = ctx
	log.Print("transitive hook ran")
}
`)
	writeTestFile(t, moduleDir, "rules.yml", `instrument_answer:
  target: example.com/otelc-transitive-import
  where:
    func: Answer
  do:
    - inject_hooks:
        before: Before
        path: example.com/otelc-transitive-import/hooks
`)

	env := append(os.Environ(), "OTELC_RULES=rules.yml")
	output := runOtelcCommand(t, moduleDir, env, otelcPath, "go", "test", "-count=1", "-v", "./...")
	require.Contains(t, output, "transitive hook ran")
}

func TestLocalHookPackageImportingSelectedDependencyKeepsHookLinked(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	const modulePath = "example.com/otelc-transitive-link"
	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module %s

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, modulePath, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))

	writeTestFile(t, moduleDir, filepath.Join("internal", "util", "util.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

func Do() int {
	return 42
}
`)

	writeTestFile(t, moduleDir, filepath.Join("internal", "log", "log.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import (
	"fmt"

	"example.com/otelc-transitive-link/internal/util"
)

func Answer() int {
	return util.Do()
}

func Print(s string) {
	fmt.Println(s)
}
`)

	writeTestFile(t, moduleDir, filepath.Join("internal", "log", "log_test.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log

import "testing"

func TestAnswer(t *testing.T) {
	if got := Answer(); got != 42 {
		t.Fatalf("Answer() = %d, want 42", got)
	}
}
`)

	writeTestFile(t, moduleDir, filepath.Join("cmd", "app", "main.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "example.com/otelc-transitive-link/internal/log"

func main() {
	_ = log.Answer()
}
`)

	writeTestFile(t, moduleDir, filepath.Join("hooks", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"example.com/otelc-transitive-link/internal/log"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func Before(ctx hook.HookContext) {
	_ = ctx
	log.Print("transitive hook ran")
}
`)

	writeTestFile(t, moduleDir, "rules.yml", `instrument_do:
  target: example.com/otelc-transitive-link/internal/util
  where:
    func: Do
  do:
    - inject_hooks:
        before: Before
        path: example.com/otelc-transitive-link/hooks
`)

	env := append(os.Environ(), "OTELC_RULES=rules.yml")

	// Ordinary setup must generate the external test file for a later go test.
	runOtelcCommand(t, moduleDir, env, otelcPath, "setup", "./internal/log")

	require.FileExists(
		t,
		filepath.Join(moduleDir, "internal", "log", "otelc.runtime_test.go"),
	)

	cmd := exec.CommandContext(
		t.Context(),
		"go",
		"test",
		"-count=1",
		"-v",
		"-toolexec="+otelcPath+" toolexec",
		"./internal/log",
	)
	cmd.Dir = moduleDir
	cmd.Env = env

	directOutput, directErr := cmd.CombinedOutput()
	require.NoError(t, directErr, "go test after otelc setup failed:\n%s", directOutput)
	require.Contains(t, string(directOutput), "transitive hook ran")

	// The generated _test.go file must not affect ordinary builds.
	runOtelcCommand(t, moduleDir, env, otelcPath, "go", "build", "./cmd/app", "./internal/log")

	output := runOtelcCommand(
		t, moduleDir, env, otelcPath,
		"go", "test", "-count=1", "-v", "./internal/log",
	)
	require.Contains(t, output, "transitive hook ran")
}
