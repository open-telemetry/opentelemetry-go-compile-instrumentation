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

// wrapNewRule wraps the calls of lib.New with replace, and lists an import of
// helper.
func wrapNewRule(replace string) string {
	return `wrap_new:
  target: "**"
  where:
    function_call: example.com/app/lib.New
  do:
    - wrap_call:
        replace: "` + replace + `"
  imports:
    helper: example.com/app/helper
`
}

// wrapDoRule wraps the calls of (*lib.Client).Do with helper.Wrap.
const wrapDoRule = `wrap_do:
  target: "**"
  where:
    method_call: example.com/app/lib.*Client.Do
  do:
    - wrap_call:
        replace: "helper.Wrap({{ . }})"
  imports:
    helper: example.com/app/helper
`

// TestRuleImportCycle builds an application where a rule adds an import of
// helper to package a, and helper imports a. Each compile only sees its own
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
		rule    string
		wantErr string
		// wantOut is the output of the application when the build succeeds.
		wantOut string
	}{
		{
			name:    "a calls the wrapped function",
			aSource: "lib.New()",
			rule:    wrapNewRule("helper.Wrap({{ . }})"),
			wantErr: `rule "wrap_new" adds import "example.com/app/helper" to "example.com/app/a", ` +
				`but example.com/app/helper imports example.com/app/a`,
		},
		{
			// The "**" target lists the rule for every file of a, but the rule
			// adds no import to a file without a call to lib.New.
			name:    "a calls another function",
			aSource: "lib.Other()",
			rule:    wrapNewRule("helper.Wrap({{ . }})"),
			wantOut: "2 3",
		},
		{
			// The instrumentation does not add an import that the code of the
			// rule does not use.
			name:    "the rule does not use the import",
			aSource: "lib.New()",
			rule:    wrapNewRule("({{ . }})"),
			wantOut: "1 2",
		},
		{
			name:    "a sets a field that a literal rule wraps",
			aSource: "lib.Config{A: 1}.A",
			rule: `set_config:
  target: "**"
  where:
    struct_literal: example.com/app/lib.Config
  do:
    - set_fields:
        field:
          - name: A
            wrap: "helper.Wrap({{ . }})"
  imports:
    helper: example.com/app/helper
`,
			wantErr: `rule "set_config" adds import "example.com/app/helper" to "example.com/app/a", ` +
				`but example.com/app/helper imports example.com/app/a`,
		},
		{
			name:    "a calls the wrapped method",
			aSource: "(&lib.Client{}).Do()",
			rule:    wrapDoRule,
			wantErr: `rule "wrap_do" adds import "example.com/app/helper" to "example.com/app/a", ` +
				`but example.com/app/helper imports example.com/app/a`,
		},
		{
			// Only the types of a tell that this Do is not (*lib.Client).Do.
			name:    "a calls a method with the same name on another type",
			aSource: "(&lib.Server{}).Do()",
			rule:    wrapDoRule,
			wantOut: "2 3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appDir := t.TempDir()
			writeTestFile(t, appDir, "go.mod", "module example.com/app\n\ngo 1.25.0\n")
			writeTestFile(t, appDir, "lib/lib.go",
				"package lib\n\nfunc New() int { return 1 }\n\nfunc Other() int { return 2 }\n\n"+
					"type Config struct{ A int }\n\n"+
					"type Client struct{}\n\nfunc (*Client) Do() int { return 1 }\n\n"+
					"type Server struct{}\n\nfunc (*Server) Do() int { return 2 }\n")
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
			writeTestFile(t, appDir, "rules.yml", "version: \"v1.0.0\"\n\n"+tt.rule)

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
			require.NoError(t, err, "the rule adds no import to a:\n%s", out)
			runOut, err := exec.CommandContext(t.Context(), bin).CombinedOutput()
			require.NoError(t, err, string(runOut))
			assert.Equal(t, tt.wantOut, strings.TrimSpace(string(runOut)))
		})
	}
}
