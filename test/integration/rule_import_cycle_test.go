// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
	"go.opentelemetry.io/otelc/tool/util"
)

// TestRuleImportCycle builds an application where a call rule adds an import
// of helper to package a, and helper imports a. Each compile only sees its own
// imports, so without a check in setup the link failed with "inittask ... is
// not schedulable", which names neither the rule nor the cycle.
func TestRuleImportCycle(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	tests := []struct {
		name string
		// aSource is the initializer of a.X.
		aSource string
		wantErr string
	}{
		{
			name:    "a calls the wrapped function",
			aSource: "lib.New()",
			wantErr: `rule "wrap_new" adds import "example.com/app/helper" to "example.com/app/a", ` +
				`but example.com/app/helper imports example.com/app/a`,
		},
		{
			// The "**" target lists the rule for every file of a, but the rule
			// adds no import to a file without a call to lib.New.
			name:    "a calls another function",
			aSource: "lib.Other()",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appDir := t.TempDir()
			writeTestFile(t, appDir, "go.mod", "module example.com/app\n\ngo 1.25.0\n")
			writeTestFile(t, appDir, "lib/lib.go",
				"package lib\n\nfunc New() int { return 1 }\n\nfunc Other() int { return 2 }\n")
			writeTestFile(t, appDir, "a/a.go",
				"package a\n\nimport \"example.com/app/lib\"\n\nvar X = "+tt.aSource+"\n")
			writeTestFile(t, appDir, "helper/helper.go",
				"package helper\n\nimport \"example.com/app/a\"\n\nfunc Wrap(v int) int { return v + a.X }\n")
			writeTestFile(t, appDir, "main.go", `package main

import (
	"example.com/app/a"
	"example.com/app/helper"
)

func main() { println(a.X, helper.Wrap(1)) }
`)
			writeTestFile(t, appDir, "rules.yml", `version: "v1.0.0"

wrap_new:
  target: "**"
  where:
    function_call: example.com/app/lib.New
  do:
    - wrap_call:
        replace: "helper.Wrap({{ . }})"
  imports:
    helper: example.com/app/helper
`)

			bin := filepath.Join(appDir, "app")
			if util.IsWindows() {
				bin += ".exe"
			}
			cmd := exec.CommandContext(t.Context(), otelcPath, "go", "build", "-o", bin, ".")
			cmd.Dir = appDir
			cmd.Env = append(os.Environ(), "OTELC_RULES=rules.yml", "GOCACHE="+t.TempDir())
			out, err := cmd.CombinedOutput()

			if tt.wantErr != "" {
				require.Error(t, err, "the build must fail:\n%s", out)
				assert.Contains(t, string(out), tt.wantErr)
				assert.NotContains(t, string(out), "not schedulable",
					"setup must report the cycle before the build reaches the linker")
				return
			}
			require.NoError(t, err, "a package without a call to lib.New gets no import:\n%s", out)
			runOut, err := exec.CommandContext(t.Context(), bin).CombinedOutput()
			require.NoError(t, err, string(runOut))
			assert.Equal(t, "2 3", strings.TrimSpace(string(runOut)))
		})
	}
}
