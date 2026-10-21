//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
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
// -pgoprofile flag, so none of them may be skipped.
func TestPGOBuildIsInstrumented(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	// Resolve symlinks (e.g. /var -> /private/var on macOS) so the recorded
	// -pgo path can be compared literally.
	moduleDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	writeTestFile(t, moduleDir, "go.mod", "module example.com/otelc-pgo\n\ngo 1.25.0\n")
	writeTestFile(t, moduleDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"net/http"
	"os"
	"runtime/pprof"
)

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
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
		fmt.Println(fib(35))
		pprof.StopCPUProfile()
		_ = f.Close()
		return
	}
	_, err := http.Get("http://127.0.0.1:1/")
	fmt.Println(err != nil)
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
	require.Contains(t, symbols, "otelc/instrumentation/net/http",
		"net/http was not instrumented in a build with default.pgo")

	buildInfo := goOutput(t, moduleDir, "version", "-m", binary)
	require.Contains(t, buildInfo, "-pgo="+profile, "the build no longer applies the PGO profile")
}

func goOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}
