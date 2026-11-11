// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
	"go.opentelemetry.io/otelc/tool/util"
)

// TestBuildRemovesGoWorkDir guards against leaking Go's temporary work directory outside debug mode.
func TestBuildRemovesGoWorkDir(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)
	t.Setenv("GOFLAGS", "")
	t.Setenv(util.EnvOtelcDebug, "")

	for _, tt := range []struct {
		name  string
		debug bool
	}{
		{name: "default"},
		{name: "debug", debug: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			moduleDir := t.TempDir()
			writeTestFile(t, moduleDir, "go.mod", "module example.com/otelc-work-dir\n\ngo 1.25.0\n")
			writeTestFile(t, moduleDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "net/http"

func main() {
	_, _ = http.Get("http://127.0.0.1:1/")
}
`)
			tmpDir, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			t.Setenv("GOTMPDIR", tmpDir)
			binary := filepath.Join(moduleDir, "app")
			args := []string{"go", "build", "-o", binary, "."}
			if tt.debug {
				args = append([]string{"--debug"}, args...)
			}
			output := runOtelcCommand(t, moduleDir, os.Environ(), otelcPath, args...)
			workLine := regexp.MustCompile(`(?m)^WORK=([^\r\n]+)\r?$`).FindStringSubmatch(output)
			if tt.debug {
				require.Len(t, workLine, 2, "debug build did not print its work directory: %s", output)
				require.Equal(t, tmpDir, filepath.Dir(workLine[1]))
				require.DirExists(t, workLine[1])
			} else {
				require.Empty(t, workLine, "default build printed a WORK line: %s", output)
				workDirs, globErr := filepath.Glob(filepath.Join(tmpDir, "go-build*"))
				require.NoError(t, globErr)
				require.Empty(t, workDirs, "default build left Go work directories behind")
			}
			symbols := goOutput(t, moduleDir, "tool", "nm", binary)
			require.Contains(t, symbols, "otelc/instrumentation/net/http", "net/http was not instrumented")
		})
	}
}

// TestBuildAcrossWorkDirsReusesCache verifies that rebuilding the same module
// with the same GOCACHE under a new --work-dir produces an identical matched.json
// and reuses cached compile archives rather than recompiling every package.
// Regression test for #1764.
func TestBuildAcrossWorkDirsReusesCache(t *testing.T) {
	t.Parallel()

	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", `module example.com/otelc-work-dir-local

go 1.25.0

require example.com/otelc-work-dir-local/hooks v0.0.0-00010101000000-000000000000

replace example.com/otelc-work-dir-local/hooks => ./hooks
`)
	writeTestFile(t, moduleDir, "otel.instrumentation.go", `//go:build tools

package tools

import _ "example.com/otelc-work-dir-local/hooks"
`)
	writeTestFile(t, moduleDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

func Greet() string { return "hello" }

func main() {
	fmt.Println(Greet())
}
`)
	writeTestFile(
		t,
		moduleDir,
		filepath.Join("hooks", "go.mod"),
		"module example.com/otelc-work-dir-local/hooks\n\ngo 1.25.0\n",
	)
	writeTestFile(t, moduleDir, filepath.Join("hooks", "hooks.go"), "package hooks\n")
	writeTestFile(t, moduleDir, filepath.Join("hooks", "otelc.yaml"), `version: "v1.0.0"

hook_greet:
  target: main
  where:
    func: Greet
  do:
    - inject_code:
        raw: 'println("hooked")'
`)

	goCache := t.TempDir()
	wdA := t.TempDir()
	wdB := t.TempDir()
	outDir := t.TempDir()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	binA := filepath.Join(outDir, "appA"+ext)
	binB := filepath.Join(outDir, "appB"+ext)

	env := append(preparedBuildBaseEnvironment(),
		"GOWORK=off",
		"GOCACHE="+goCache,
		"OTELC_STATS=1",
	)

	runOtelcCommand(t, moduleDir, env, otelcPath, "--work-dir", wdA, "go", "build", "-o", binA, ".")
	runOtelcCommand(t, moduleDir, env, otelcPath, "--work-dir", wdB, "go", "build", "-o", binB, ".")

	matchedA, err := os.ReadFile(filepath.Join(wdA, util.BuildTempDir, "matched.json"))
	require.NoError(t, err)
	matchedB, err := os.ReadFile(filepath.Join(wdB, util.BuildTempDir, "matched.json"))
	require.NoError(t, err)
	assert.Contains(
		t,
		string(matchedA),
		`"file":"runtime_gls.go","path":"\u003cinternal\u003e","resolved_path":""`,
	)
	assert.Equal(t, string(matchedA), string(matchedB), "matched.json must not depend on --work-dir")

	logA, err := os.ReadFile(filepath.Join(wdA, util.BuildTempDir, "debug.log"))
	require.NoError(t, err)
	logB, err := os.ReadFile(filepath.Join(wdB, util.BuildTempDir, "debug.log"))
	require.NoError(t, err)

	assert.Contains(t, string(logA), "package=runtime", "cold build should compile runtime")
	assert.Contains(t, string(logA), "Apply file rule", "cold build should apply runtime_gls.go file rule")
	assert.NotContains(t, string(logB), "package=runtime", "rebuild in new work-dir must reuse GOCACHE for runtime")
	assert.NotContains(
		t,
		string(logB),
		"package=example.com/otelc-work-dir-local",
		"rebuild in new work-dir must reuse GOCACHE for main",
	)

	runCmd := exec.CommandContext(t.Context(), binB)
	runOut, runErr := runCmd.CombinedOutput()
	require.NoError(t, runErr, "running cached binary failed:\n%s", runOut)
	assert.Contains(t, string(runOut), "hooked")
}
