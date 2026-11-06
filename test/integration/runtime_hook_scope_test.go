//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

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

// TestRuntimeHookScope builds two packages in one invocation. Each package uses
// a different instrumented library, and each binary must link only the hook
// package of the library it uses. The hook packages record themselves in a
// registry when they are initialized, so each binary reports the hooks it
// links.
func TestRuntimeHookScope(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	const module = "example.com/otelc-hook-scope"
	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module %s

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, module, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))
	writeTestFile(t, moduleDir, filepath.Join("registry", "registry.go"), `// Copyright The OpenTelemetry Authors

package registry

import "strings"

var loaded []string

func Add(name string) { loaded = append(loaded, name) }

func Loaded() string { return strings.Join(loaded, ",") }
`)

	rules := ""
	for _, name := range []string{"a", "b"} {
		upper := strings.ToUpper(name)
		writeTestFile(
			t,
			moduleDir,
			filepath.Join("lib"+name, "lib.go"),
			fmt.Sprintf(`// Copyright The OpenTelemetry Authors

package lib%[1]s

func %[2]s() string { return %[1]q }
`, name, upper),
		)
		writeTestFile(
			t,
			moduleDir,
			filepath.Join("hook"+name, "hooks.go"),
			fmt.Sprintf(`// Copyright The OpenTelemetry Authors

package hook%[1]s

import (
	"go.opentelemetry.io/otelc/pkg/hook"

	"%[3]s/registry"
)

func init() { registry.Add("hook%[1]s") }

func Before%[2]s(ctx hook.HookContext) { _ = ctx }
`, name, upper, module),
		)
		writeTestFile(t, moduleDir, filepath.Join(name, name+".go"), fmt.Sprintf(`// Copyright The OpenTelemetry Authors

package %[1]s

import "%[3]s/lib%[1]s"

func Run() string { return lib%[1]s.%[2]s() }
`, name, upper, module))
		writeTestFile(
			t,
			moduleDir,
			filepath.Join(name, name+"_test.go"),
			fmt.Sprintf(`// Copyright The OpenTelemetry Authors

package %[1]s

import (
	"testing"

	"%[2]s/registry"
)

func TestHooks(t *testing.T) {
	_ = Run()
	if got := registry.Loaded(); got != "hook%[1]s" {
		t.Fatalf("linked hooks = %%q, want %%q", got, "hook%[1]s")
	}
}
`, name, module),
		)
		writeTestFile(
			t,
			moduleDir,
			filepath.Join("cmd", name, "main.go"),
			fmt.Sprintf(`// Copyright The OpenTelemetry Authors

package main

import (
	"fmt"

	"%[2]s/%[1]s"
	"%[2]s/registry"
)

func main() {
	_ = %[1]s.Run()
	fmt.Println("linked hooks:", registry.Loaded())
}
`, name, module),
		)
		rules += fmt.Sprintf(`instrument_%[1]s:
  target: %[3]s/lib%[1]s
  where:
    func: %[2]s
  do:
    - inject_hooks:
        before: Before%[2]s
        path: %[3]s/hook%[1]s
`, name, upper, module)
	}
	writeTestFile(t, moduleDir, "rules.yml", rules)

	env := append(os.Environ(), "OTELC_RULES=rules.yml")

	t.Run("test", func(t *testing.T) {
		output := runOtelcCommand(t, moduleDir, env, otelcPath, "go", "test", "-count=1", "-v", "./a", "./b")
		require.Contains(t, output, "ok  \t"+module+"/a")
		require.Contains(t, output, "ok  \t"+module+"/b")
	})

	t.Run("build", func(t *testing.T) {
		binDir := t.TempDir()
		runOtelcCommand(t, moduleDir, env, otelcPath, "go", "build", "-o", binDir+string(filepath.Separator),
			"./cmd/a", "./cmd/b")
		for _, name := range []string{"a", "b"} {
			bin := filepath.Join(binDir, name)
			if _, statErr := os.Stat(bin + ".exe"); statErr == nil {
				bin += ".exe"
			}
			output, runErr := exec.CommandContext(t.Context(), bin).CombinedOutput()
			require.NoError(t, runErr, "%s failed:\n%s", bin, output)
			require.Equal(t, "linked hooks: hook"+name+"\n", strings.ReplaceAll(string(output), "\r\n", "\n"))
		}
	})
}
