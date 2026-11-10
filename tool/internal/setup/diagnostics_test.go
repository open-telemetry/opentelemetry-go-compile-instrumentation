// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestMatchDiagnostics(t *testing.T) {
	for _, code := range []string{
		"", "version_mismatch", "no_source_files", "where_mismatch",
		"function_not_found", "receiver_mismatch", "signature_mismatch", "struct_not_found",
		"directive_not_found", "declaration_not_found",
	} {
		t.Run("reason="+code, func(t *testing.T) {
			t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
			t.Setenv(util.EnvOtelcDebug, "1")
			source := writeGoSource(t, "source.go", "package p; func F(x int) {}")
			dep := &Dependency{ImportPath: "example.com/p", Version: "v1.0.0", Sources: []string{source}}
			base := rule.InstBaseRule{Name: "candidate", Target: rule.NewTarget(dep.ImportPath)}
			fr := &rule.InstFuncRule{InstBaseRule: base, Func: "F"}
			var r rule.InstRule = fr
			switch code {
			case "version_mismatch":
				fr.Version = "v2.0.0,v3.0.0"
			case "no_source_files":
				dep.Sources = nil
			case "where_mismatch":
				fr.Where = &rule.WhereDef{File: &rule.FilterDef{HasFunc: "Missing"}}
			case "function_not_found":
				fr.Func = "Missing"
			case "receiver_mismatch":
				fr.Recv = "*T"
			case "signature_mismatch":
				fr.Signature = &rule.FuncSignature{Args: []string{"string"}}
			case "struct_not_found":
				r = &rule.InstStructRule{InstBaseRule: base, Struct: "Missing"}
			case "directive_not_found":
				r = &rule.InstDirectiveRule{InstBaseRule: base, Directive: "otelc:missing"}
			case "declaration_not_found":
				r = &rule.InstDeclRule{InstBaseRule: base, Identifier: "Missing", Kind: "var"}
			}
			exact := map[string][]rule.InstRule{dep.ImportPath: {r}}
			set, err := newTestSetupPhase().runMatch(t.Context(), dep, exact, nil)
			require.NoError(t, err)
			require.Equal(t, code != "", set.IsEmpty())
			data, err := os.ReadFile(filepath.Join(setupDebugDir(dep.ImportPath), "setup.matches.jsonl"))
			require.NoError(t, err)
			var row struct {
				Package string `json:"package"`
				Rule    string `json:"rule"`
				Source  string `json:"source"`
				Reason  string `json:"reason"`
				Phase   string `json:"phase"`
				Matched bool   `json:"matched"`
			}
			require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(string(data))), &row))
			require.Equal(t, code, row.Reason)
			require.Equal(t, code == "", row.Matched)
			require.Equal(t, dep.ImportPath, row.Package)
			require.Equal(t, "candidate", row.Rule)
			require.Equal(t, "setup", row.Phase)
		})
	}
}

// A glob target that misses a dependency must not write a record for it,
// or every glob rule would add a row for every package in the build.
func TestMatchDiagnosticsOmitTargetMisses(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcDebug, "1")
	source := writeGoSource(t, "source.go", "package p; func F(x int) {}")
	dep := &Dependency{ImportPath: "example.com/p", Version: "v1.0.0", Sources: []string{source}}
	target := rule.NewTarget("other/*")
	r := &rule.InstFuncRule{InstBaseRule: rule.InstBaseRule{Name: "candidate", Target: target}, Func: "F"}
	set, err := newTestSetupPhase().runMatch(t.Context(), dep, nil, []targetRule{{target: &target, rule: r}})
	require.NoError(t, err)
	require.True(t, set.IsEmpty())
	require.NoFileExists(t, filepath.Join(setupDebugDir(dep.ImportPath), "setup.matches.jsonl"))
}

func TestMatchDiagnosticsDisabled(t *testing.T) {
	for _, value := range []string{"", "0", "false", "invalid"} {
		t.Run("debug="+value, func(t *testing.T) {
			t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
			t.Setenv(util.EnvOtelcDebug, value)
			dep := &Dependency{ImportPath: "example.com/p"}
			r := &rule.InstFuncRule{InstBaseRule: rule.InstBaseRule{Name: "candidate"}, Func: "F"}
			_, err := newTestSetupPhase().runMatch(t.Context(), dep, map[string][]rule.InstRule{dep.ImportPath: {r}}, nil)
			require.NoError(t, err)
			require.NoDirExists(t, setupDebugDir(dep.ImportPath))
		})
	}
}

// A diagnostics write failure is a warning: debug output must never fail the
// build.
func TestMatchDiagnosticsWriteFailure(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcDebug, "1")
	source := writeGoSource(t, "source.go", "package p; func F(x int) {}")
	dep := &Dependency{ImportPath: "example.com/p", Version: "v1.0.0", Sources: []string{source}}
	dir := setupDebugDir(dep.ImportPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(dir), 0o755))
	require.NoError(t, os.WriteFile(dir, nil, 0o600))

	var logs bytes.Buffer
	sp := &setupPhase{logger: slog.New(slog.NewTextHandler(&logs, nil))}
	r := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "candidate", Target: rule.NewTarget(dep.ImportPath)},
		Func:         "Missing",
	}
	set, err := sp.runMatch(t.Context(), dep, map[string][]rule.InstRule{dep.ImportPath: {r}}, nil)
	require.NoError(t, err)
	require.True(t, set.IsEmpty())
	require.Contains(t, logs.String(), "failed to write rule diagnostics")
}
