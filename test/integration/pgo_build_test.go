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

// TestPGOBuildIsInstrumented checks that a main package with a default.pgo is
// still instrumented. With -pgo=auto (the default) every compile command gets a
// -pgoprofile flag, so none of them may be skipped. The profile must make library
// code hot: one hot only in main hid a fingerprint mismatch between the outer and
// the nested build. The workload runs for a fixed time, because a short profile
// has too few samples to make library code hot.
func TestPGOBuildIsInstrumented(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	repoRootSlash := filepath.ToSlash(repoRoot)

	// Resolve symlinks (e.g. /var -> /private/var on macOS) so the recorded
	// -pgo path can be compared literally.
	moduleDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-pgo

go 1.25.0

require (
	example.com/otelc-pgo/hooks v0.0.0
	go.opentelemetry.io/otelc/pkg v0.0.0
)

replace (
	example.com/otelc-pgo/hooks => ./hooks
	go.opentelemetry.io/otelc => %s
	go.opentelemetry.io/otelc/pkg => %s/pkg
)
`, repoRootSlash, repoRootSlash))
	writeTestFile(t, moduleDir, "otel.instrumentation.go", `//go:build tools

package tools

import _ "example.com/otelc-pgo/hooks"
`)
	writeTestFile(t, moduleDir, filepath.Join("hooks", "go.mod"), fmt.Sprintf(`module example.com/otelc-pgo/hooks

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
	fmt.Println("hook executed")
}
`)
	writeTestFile(t, moduleDir, filepath.Join("hooks", "otelc.yaml"), `hook_work:
  target: example.com/otelc-pgo/lib
  where:
    func: Work
  do:
    - inject_hooks:
        before: Before
        path: example.com/otelc-pgo/hooks
`)
	writeTestFile(t, moduleDir, filepath.Join("lib", "lib.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package lib

func Work() int {
	s := 0
	for i := 0; i < 10000; i++ {
		s += i
	}
	return s
}
`)
	writeTestFile(t, moduleDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"runtime/pprof"
	"time"

	"example.com/otelc-pgo/lib"
)

func runWork() {
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		_ = lib.Work()
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "profile" {
		f, err := os.Create(os.Args[2])
		if err != nil {
			panic(err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			panic(err)
		}
		runWork()
		pprof.StopCPUProfile()
		_ = f.Close()
		return
	}
	fmt.Println(lib.Work())
}
`)

	// Record a real CPU profile with a plain go build; the compiler rejects
	// profiles it cannot parse.
	profile := filepath.Join(moduleDir, "default.pgo")
	gen := exec.Command("go", "run", ".", "profile", profile)
	gen.Dir = moduleDir
	out, err := gen.CombinedOutput()
	require.NoError(t, err, string(out))

	binary := filepath.Join(moduleDir, "app")
	runOtelcCommand(t, moduleDir, os.Environ(), otelcPath, "go", "build", "-o", binary, ".")

	symbols := goOutput(t, moduleDir, "tool", "nm", binary)
	require.Contains(t, symbols, "example.com/otelc-pgo/hooks.Before",
		"hook was not instrumented in a build with default.pgo")

	buildInfo := goOutput(t, moduleDir, "version", "-m", binary)
	require.Contains(t, buildInfo, "-pgo="+profile, "the build no longer applies the PGO profile")

	run := exec.CommandContext(t.Context(), binary)
	run.Dir = moduleDir
	runOut, runErr := run.CombinedOutput()
	require.NoError(t, runErr, string(runOut))
	require.Contains(t, string(runOut), "hook executed")
}

func goOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}
