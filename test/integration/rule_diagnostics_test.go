//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuleSignatureDiagnostics(t *testing.T) {
	name := "otelc"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	otelcPath, err := filepath.Abs(filepath.Join("..", "..", name))
	require.NoError(t, err)
	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", "module example.com/diagnostics\n\ngo 1.26.0\n")
	writeTestFile(t, dir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package diagnostics
func F(x int) {}
`)
	writeTestFile(t, dir, "rules.yml", `wrong_signature:
  target: example.com/diagnostics
  where:
    func: F
    signature:
      args: [string]
  do:
    - inject_hooks:
        before: Before
        path: example.com/unused
`)
	runOtelcCommand(t, dir, append(os.Environ(), "OTELC_RULES=rules.yml", "OTELC_DEBUG=1"), otelcPath, "go", "build", ".")
	data, err := os.ReadFile(filepath.Join(dir, ".otelc-build", "debug", "example_com_diagnostics", "setup.matches.jsonl"))
	require.NoError(t, err)
	found := false
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		var row struct {
			Package string `json:"package"`
			Rule    string `json:"rule"`
			Reason  string `json:"reason"`
			Matched bool   `json:"matched"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &row))
		if row.Rule == "wrong_signature" && row.Package == "example.com/diagnostics" {
			require.False(t, row.Matched)
			require.Equal(t, "signature_mismatch", row.Reason)
			found = true
		}
	}
	require.True(t, found, "real setup matcher must report the rejected signature")
}
