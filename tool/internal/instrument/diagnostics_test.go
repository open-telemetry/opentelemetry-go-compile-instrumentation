// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestInstrumentMatchDiagnostics(t *testing.T) {
	for _, code := range []string{
		"ignored", "no_joinpoint", "signature_mismatch", "receiver_mismatch", "function_not_found",
	} {
		t.Run(code, func(t *testing.T) {
			t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
			t.Setenv(util.EnvOtelcDebug, "true")
			source := "package p; func F(x int) {}"
			base := rule.InstBaseRule{Name: "candidate"}
			fr := &rule.InstFuncRule{InstBaseRule: base, Func: "F"}
			var r rule.InstRule = fr
			switch code {
			case "ignored":
				source = "//otelc:ignore\npackage p; func F(x int) {}"
			case "no_joinpoint":
				r = &rule.InstCallRule{InstBaseRule: base, FunctionCall: "missing.F"}
			case "signature_mismatch":
				fr.Signature = &rule.FuncSignature{Args: []string{"string"}}
			case "receiver_mismatch":
				fr.Recv = "*T"
			case "function_not_found":
				fr.Func = "Missing"
			}
			root, err := ast.NewAstParser().ParseSource(source)
			require.NoError(t, err)
			ip := newTestPhase()
			ip.compileArgs = []string{"-p", "example.com/p"}
			ip.diagnosticSource = "source.go"
			_, _, applyErr := ip.applyRulesCapturingDiffs(t.Context(), []rule.InstRule{r}, root)
			if code == "ignored" || code == "no_joinpoint" {
				require.NoError(t, applyErr)
			} else {
				require.Error(t, applyErr)
			}
			data, err := os.ReadFile(filepath.Join(ip.debugArtifactDir(), "source.go.matches.jsonl"))
			require.NoError(t, err)
			var row struct {
				Reason  string `json:"reason"`
				Phase   string `json:"phase"`
				Source  string `json:"source"`
				Matched bool   `json:"matched"`
			}
			require.NoError(t, json.Unmarshal(data, &row))
			require.Equal(t, code, row.Reason)
			require.False(t, row.Matched)
			require.Equal(t, "instrument", row.Phase)
			require.Equal(t, "source.go", row.Source)
		})
	}
}

func TestInstrumentMatchDiagnosticsImplements(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcDebug, "true")
	ip, root := setupImplementsPhase(t, implementsSource)
	ip.diagnosticSource = "source.go"
	r := &rule.InstFuncRule{
		InstBaseRule:    rule.InstBaseRule{Name: "candidate"},
		Func:            "noMatch",
		ParamImplements: "context.Context",
	}
	result, _, err := ip.applyRulesCapturingDiffs(t.Context(), []rule.InstRule{r}, root)
	require.NoError(t, err)
	require.False(t, result.modified)

	data, err := os.ReadFile(filepath.Join(ip.debugArtifactDir(), "source.go.matches.jsonl"))
	require.NoError(t, err)
	var row struct {
		Rule    string `json:"rule"`
		Reason  string `json:"reason"`
		Matched bool   `json:"matched"`
	}
	require.NoError(t, json.Unmarshal(data, &row))
	require.Equal(t, "candidate", row.Rule)
	require.Equal(t, "signature_mismatch", row.Reason)
	require.False(t, row.Matched)
}
