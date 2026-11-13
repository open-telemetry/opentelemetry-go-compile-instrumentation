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

// TestInjectCodeRaisesLanguageVersion verifies that an inject_code modifier with
// a lang field raises the target package's -lang flag so newer language features
// (such as 'any' or generic calls) compile even when the target declares an older
// Go version in its go.mod.
func TestInjectCodeRaisesLanguageVersion(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	tempDir := t.TempDir()

	oldLibDir := filepath.Join(tempDir, "oldlib")
	require.NoError(t, os.MkdirAll(oldLibDir, 0o755))
	writeTestFile(t, oldLibDir, "go.mod", "module example.com/oldlib\n\ngo 1.7\n")
	writeTestFile(t, oldLibDir, "lib.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package oldlib

func Greeting(name string) string {
	return "hello " + name
}
`)

	appDir := filepath.Join(tempDir, "app")
	require.NoError(t, os.MkdirAll(appDir, 0o755))
	writeTestFile(t, appDir, "go.mod", fmt.Sprintf(`module example.com/app

go 1.25.0

require (
	example.com/oldlib v0.0.0
	go.opentelemetry.io/otelc/pkg v0.0.0
)

replace example.com/oldlib => ../oldlib
replace go.opentelemetry.io/otelc/pkg => %s
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))

	writeTestFile(t, appDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"example.com/oldlib"
)

func main() {
	fmt.Println(oldlib.Greeting("world"))
}
`)

	rulesPath := filepath.Join(appDir, "rules.yml")
	writeTestFile(t, appDir, "rules.yml", `inject_any:
  target: example.com/oldlib
  where:
    func: Greeting
  do:
    - inject_code:
        lang: go1.18
        raw: |-
          var tag any = name
          _ = tag
`)

	outputPath := filepath.Join(appDir, "app_bin")
	if util.IsWindows() {
		outputPath += ".exe"
	}
	env := append(os.Environ(), "OTELC_RULES="+rulesPath)
	runOtelcCommand(t, appDir, env, otelcPath, "go", "build", "-o", outputPath, ".")

	runCmd := exec.CommandContext(t.Context(), outputPath)
	runCmd.Dir = appDir
	runOut, err := runCmd.CombinedOutput()
	require.NoError(t, err, "running built binary failed: %s", string(runOut))
	require.Contains(t, string(runOut), "hello world")
}
