//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

// TestRuleImportNotInModule builds an application with a rule that imports a
// package no module of the build provides. The build must fail, and the
// message must name the rule and the import, so the author of the rule file
// can find the line to fix.
func TestRuleImportNotInModule(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	tests := []struct {
		name string
		rule string
	}{
		{
			name: "inject_code",
			rule: `needs_ghost:
  target: example.com/app
  where:
    func: Work
  do:
    - inject_code:
        raw: "_ = ghost.Value"
  imports:
    ghost: example.com/missing/ghost
`,
		},
		{
			name: "blank import",
			rule: `needs_ghost:
  target: example.com/app
  where:
    func: Work
  do:
    - inject_code:
        raw: "_ = 1"
  imports:
    _: example.com/missing/ghost
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appDir := t.TempDir()
			writeTestFile(t, appDir, "go.mod", "module example.com/app\n\ngo 1.25.0\n")
			writeTestFile(t, appDir, "work.go", "package app\n\nfunc Work() int { return 1 }\n")
			writeTestFile(t, appDir, "main/main.go",
				"package main\n\nimport \"example.com/app\"\n\nfunc main() { println(app.Work()) }\n")
			writeTestFile(t, appDir, "rules.yml", "version: \"v1.0.0\"\n\n"+tt.rule)

			bin := filepath.Join(appDir, "app")
			cmd := exec.CommandContext(t.Context(), otelcPath, "go", "build", "-o", bin, "./main")
			cmd.Dir = appDir
			cmd.Env = append(os.Environ(), "OTELC_RULES=rules.yml", "GOCACHE="+t.TempDir())
			out, buildErr := cmd.CombinedOutput()

			require.Error(t, buildErr, "the build must fail:\n%s", out)
			assert.Contains(t, string(out), "needs_ghost")
			assert.Contains(t, string(out), "example.com/missing/ghost")
		})
	}
}
