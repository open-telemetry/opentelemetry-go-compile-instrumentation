// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

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

// TestGcflagsBuildLinks builds an instrumented application with -gcflags.
// otelc adds an import to main that main did not have, and toolexec resolves
// it with a nested `go list -export`. When that build compiles the import with
// other compiler flags than the outer build, the link fails with "fingerprint
// mismatch". The nested build must apply a patterned value such as the
// debugger's all=-N -l, and must not apply an unpatterned value, from the
// command line or from GOFLAGS: that one is meant for the build's own targets.
func TestGcflagsBuildLinks(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	repoRootSlash := filepath.ToSlash(repoRoot)

	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-gcflags

go 1.25.0

require (
	example.com/otelc-gcflags/hooks v0.0.0
	go.opentelemetry.io/otelc/pkg v0.0.0
)

replace (
	example.com/otelc-gcflags/hooks => ./hooks
	go.opentelemetry.io/otelc => %s
	go.opentelemetry.io/otelc/pkg => %s/pkg
)
`, repoRootSlash, repoRootSlash))
	writeTestFile(t, moduleDir, "otel.instrumentation.go", `//go:build tools

package tools

import _ "example.com/otelc-gcflags/hooks"
`)
	writeTestFile(t, moduleDir, filepath.Join("hooks", "go.mod"), fmt.Sprintf(`module example.com/otelc-gcflags/hooks

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s/pkg
`, repoRootSlash))
	writeTestFile(t, moduleDir, filepath.Join("hooks", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"fmt"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func Before(ctx hook.HookContext) {
	_ = ctx
	fmt.Println("hook called")
}
`)
	writeTestFile(t, moduleDir, filepath.Join("hooks", "otelc.yaml"), `hook_target:
  target: main
  where:
    func: Target
  do:
    - inject_hooks:
        before: Before
        path: example.com/otelc-gcflags/hooks
`)
	writeTestFile(t, moduleDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

func Target() {
	fmt.Println("target called")
}

func main() {
	Target()
}
`)

	tests := []struct {
		name    string
		args    []string
		goflags string
		// recorded is how `go version -m` reports the flag in the binary.
		recorded string
	}{
		{name: "debugger", args: []string{"-gcflags=all=-N -l"}, recorded: `-gcflags="all=-N -l"`},
		{name: "unpatterned_cli", args: []string{"-gcflags=-N -l"}, recorded: `-gcflags="-N -l"`},
		{name: "unpatterned_goflags", goflags: "-gcflags=-l", recorded: "-gcflags=-l"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GOFLAGS", tt.goflags)
			binary := filepath.Join(moduleDir, tt.name)
			if util.IsWindows() {
				binary += ".exe"
			}
			args := append([]string{"go", "build", "-o", binary}, tt.args...)
			runOtelcCommand(t, moduleDir, os.Environ(), otelcPath, append(args, ".")...)

			symbols := goOutput(t, moduleDir, "tool", "nm", binary)
			require.Contains(t, symbols, "example.com/otelc-gcflags/hooks.Before", "hook was not linked")
			buildInfo := goOutput(t, moduleDir, "version", "-m", binary)
			require.Contains(t, buildInfo, tt.recorded, "the binary was not built with the requested flags")

			run := exec.CommandContext(t.Context(), binary)
			run.Dir = moduleDir
			out, runErr := run.CombinedOutput()
			require.NoError(t, runErr, string(out))
			require.Contains(t, string(out), "hook called")
			require.Contains(t, string(out), "target called")
		})
	}
}
