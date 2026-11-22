// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/compileindex"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func indexedCompile(t *testing.T) []string {
	t.Helper()
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), []byte(`[]`), 0o644))
	require.NoError(t, os.WriteFile(util.GetImportNamesFile(), []byte(`{}`), 0o644))
	require.NoError(t, compileindex.Store())
	return []string{"compile", "-o", filepath.Join(t.TempDir(), "out.a"), "-p", "main"}
}

func TestIndexedCandidatesKeepExactMatching(t *testing.T) {
	indexedCompile(t)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go")
	normal := rule.NewInstRuleSet("main")
	normal.AddFuncRule(a, &rule.InstFuncRule{Func: "Target"})
	otherMain := rule.NewInstRuleSet("main")
	otherMain.AddFuncRule(b, &rule.InstFuncRule{Func: "Target"})
	test := rule.NewInstRuleSet("main")
	test.IsTest = true
	test.AddFuncRule(a, &rule.InstFuncRule{Func: "Target"})
	cgo := rule.NewInstRuleSet("example.com/cgo")
	cgo.AddFuncRule(filepath.Join(dir, "old.cgo1.go"), &rule.InstFuncRule{Func: "Target"})
	cgo.CgoFileMap = map[string]string{filepath.Join(dir, "old.cgo1.go"): "new.cgo1.go"}
	ignored := rule.NewInstRuleSet("example.com/other")
	ignored.AddIgnoredCallFile(filepath.Join(dir, "ignored.go"))
	sets := []*rule.InstRuleSet{normal, otherMain, test, cgo, ignored}
	data, err := json.Marshal(sets)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), data, 0o644))
	require.NoError(t, compileindex.Store())
	ip := &instrumentPhase{logger: slog.Default(), workDir: dir}
	loaded, err := ip.load()
	require.NoError(t, err)
	assert.True(t, anyRuleSetHasIgnoredCallFiles(loaded))
	for _, tt := range []struct {
		path      string
		files     []string
		wantIndex int
	}{
		{"main", []string{a}, 0},
		{"main", []string{b}, 1},
		{"main", []string{a, filepath.Join(dir, "a_test.go")}, 2},
		{"main", []string{filepath.Join(dir, "unrelated.go")}, -1},
		{"example.com/cgo", []string{filepath.Join(dir, "new.cgo1.go")}, 3},
	} {
		assert.False(t, compileindex.Excludes(tt.path))
		args := append([]string{"compile", "-p", tt.path}, tt.files...)
		got := ip.match(loaded, args)
		if tt.wantIndex < 0 {
			assert.Nil(t, got)
		} else {
			assert.Same(t, loaded[tt.wantIndex], got)
		}
	}
}

func TestIndexedCompilePassThrough(t *testing.T) {
	args := indexedCompile(t)
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	assert.True(t, compileindex.Excludes("main"))
	got, err := interceptCompile(ctx, args)
	require.NoError(t, err)
	assert.Equal(t, args, got)
}

func TestIndexedCompilePreservesArtifactErrors(t *testing.T) {
	for _, artifact := range []string{"matched", "names"} {
		t.Run(artifact, func(t *testing.T) {
			args := indexedCompile(t)
			path := util.GetMatchedRuleFile()
			if artifact == "names" {
				path = util.GetImportNamesFile()
			}
			require.NoError(t, os.WriteFile(path, []byte("not JSON"), 0o644))
			ctx := util.ContextWithLogger(t.Context(), slog.Default())
			_, err := interceptCompile(ctx, args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unmarshal")
		})
	}
}

func TestIndexedCompilePreservesMissingMatchedError(t *testing.T) {
	args := indexedCompile(t)
	require.NoError(t, os.Remove(util.GetMatchedRuleFile()))
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	_, err := interceptCompile(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "otelc setup")
}

func TestIndexedCompilePreservesMissingNamesFallback(t *testing.T) {
	args := indexedCompile(t)
	require.NoError(t, os.Remove(util.GetImportNamesFile()))
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	got, err := interceptCompile(ctx, args)
	require.NoError(t, err)
	assert.Equal(t, args, got)
}

func TestIndexedCompileStillParsesImportCfg(t *testing.T) {
	args := indexedCompile(t)
	args = append(args, "-importcfg", filepath.Join(t.TempDir(), "missing.importcfg"))
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	_, err := interceptCompile(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "importcfg")
}
