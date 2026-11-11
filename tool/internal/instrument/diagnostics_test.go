// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

func TestInstrumentMatchDiagnosticsApplicationOutcome(t *testing.T) {
	directive := &rule.InstDirectiveRule{
		InstBaseRule: rule.InstBaseRule{Name: "candidate"},
		Directive:    "otelc:test",
		Template:     `println("{{ .FuncName }}")`,
	}
	call := httpGetRule("traced({{ . }})")
	call.Name = "candidate"
	tests := []struct {
		name    string
		source  string
		r       rule.InstRule
		matched bool
		reason  string
	}{
		{
			"every call site ignored",
			"package p\n\nimport \"net/http\"\n\nfunc Run() {\n\t//otelc:ignore\n\thttp.Get(\"x\")\n}\n",
			call, false, "ignored",
		},
		{
			"one call site ignored, one instrumented",
			"package p\n\nimport \"net/http\"\n\nfunc Run() {\n\t//otelc:ignore\n\thttp.Get(\"x\")\n\thttp.Get(\"y\")\n}\n",
			call,
			true,
			"",
		},
		{
			"directive above a variable",
			"package p\n\n//otelc:test\nvar x = 1\n",
			directive, false, "no_joinpoint",
		},
		{
			"directive above a function",
			"package p\n\n//otelc:test\nfunc F() {}\n",
			directive, true, "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
			t.Setenv(util.EnvOtelcDebug, "true")
			ip := newTestPhase()
			ip.parser = ast.NewAstParser()
			root, err := ip.parser.ParseSource(tt.source)
			require.NoError(t, err)
			ip.compileArgs = []string{"-p", "example.com/p"}
			ip.diagnosticSource = "source.go"
			_, _, err = ip.applyRulesCapturingDiffs(t.Context(), []rule.InstRule{tt.r}, root)
			require.NoError(t, err)

			data, err := os.ReadFile(filepath.Join(ip.debugArtifactDir(), "source.go.matches.jsonl"))
			require.NoError(t, err)
			var row struct {
				Reason  string `json:"reason"`
				Matched bool   `json:"matched"`
			}
			require.NoError(t, json.Unmarshal(data, &row))
			require.Equal(t, tt.reason, row.Reason)
			require.Equal(t, tt.matched, row.Matched)
		})
	}
}

func TestInstrumentMatchDiagnosticsFunctionIgnoreAndRawRules(t *testing.T) {
	base := rule.InstBaseRule{Name: "candidate"}
	tests := []struct {
		name    string
		source  string
		r       rule.InstRule
		wantErr bool
		reason  string
	}{
		{
			"function-level ignore",
			"package p\n\n//otelc:ignore\nfunc F(x int) {}\n",
			&rule.InstFuncRule{InstBaseRule: base, Func: "F"}, false, "ignored",
		},
		{
			"raw rule function not found",
			"package p\n\nfunc F(x int) {}\n",
			&rule.InstRawRule{InstBaseRule: base, Func: "Missing", Raw: "println()"}, true, "function_not_found",
		},
		{
			"raw rule signature mismatch",
			"package p\n\nfunc F(x int) {}\n",
			&rule.InstRawRule{
				InstBaseRule: base, Func: "F", Raw: "println()",
				Signature: &rule.FuncSignature{Args: []string{"string"}},
			},
			true, "signature_mismatch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
			t.Setenv(util.EnvOtelcDebug, "true")
			ip := newTestPhase()
			ip.parser = ast.NewAstParser()
			root, err := ip.parser.ParseSource(tt.source)
			require.NoError(t, err)
			ip.compileArgs = []string{"-p", "example.com/p"}
			ip.diagnosticSource = "source.go"
			result, _, err := ip.applyRulesCapturingDiffs(t.Context(), []rule.InstRule{tt.r}, root)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.False(t, result.modified)
			}

			data, err := os.ReadFile(filepath.Join(ip.debugArtifactDir(), "source.go.matches.jsonl"))
			require.NoError(t, err)
			// One record per rule: the ignored func rule is not reported again
			// as applied.
			require.Equal(t, 1, strings.Count(string(data), "\n"))
			var row struct {
				Reason  string `json:"reason"`
				Matched bool   `json:"matched"`
			}
			require.NoError(t, json.Unmarshal(data, &row))
			require.Equal(t, tt.reason, row.Reason)
			require.False(t, row.Matched)
		})
	}
}

// A diagnostics write failure is a warning: debug output must never fail the
// build.
func TestInstrumentMatchDiagnosticsWriteFailure(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcDebug, "true")
	var logs bytes.Buffer
	ip := &instrumentPhase{
		logger:           slog.New(slog.NewTextHandler(&logs, nil)),
		compileArgs:      []string{"-p", "example.com/p"},
		diagnosticSource: "source.go",
	}
	dir := ip.debugArtifactDir()
	require.NoError(t, os.MkdirAll(filepath.Dir(dir), 0o755))
	require.NoError(t, os.WriteFile(dir, nil, 0o600))

	ip.reportDecision(&rule.InstFuncRule{InstBaseRule: rule.InstBaseRule{Name: "candidate"}}, "ignored")
	require.Contains(t, logs.String(), "failed to write rule diagnostics")
}
