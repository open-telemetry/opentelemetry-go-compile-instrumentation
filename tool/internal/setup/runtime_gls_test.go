// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/internal/instrument"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	toolruntime "go.opentelemetry.io/otelc/tool/internal/runtime"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestMatchRuntimeGLS_NoRuntimeDep(t *testing.T) {
	deps := []*Dependency{
		{ImportPath: "example.com/other"},
	}

	sets, err := matchRuntimeGLS(deps)
	require.NoError(t, err)
	assert.Nil(t, sets)
}

func TestMatchRuntimeGLS_WithRuntimeDep(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	mockRuntimeDir := t.TempDir()
	runtime2File := filepath.Join(mockRuntimeDir, "runtime2.go")
	procFile := filepath.Join(mockRuntimeDir, "proc.go")
	otherFile := filepath.Join(mockRuntimeDir, "other.go")

	require.NoError(t, os.WriteFile(runtime2File, []byte("package runtime\n"), 0o644))
	require.NoError(t, os.WriteFile(procFile, []byte("package runtime\n"), 0o644))
	require.NoError(t, os.WriteFile(otherFile, []byte("package runtime\n"), 0o644))

	deps := []*Dependency{
		{
			ImportPath: toolruntime.TargetRuntime,
			Sources:    []string{runtime2File, procFile, otherFile},
		},
	}

	sets, err := matchRuntimeGLS(deps)
	require.NoError(t, err)
	require.Len(t, sets, 1)

	rset := sets[0]
	assert.Equal(t, toolruntime.TargetRuntime, rset.ModulePath)
	assert.Equal(t, toolruntime.TargetRuntime, rset.PackageName)

	assert.Len(t, rset.StructRules[runtime2File], 1)
	assert.Equal(t, "add_gls_field", rset.StructRules[runtime2File][0].Name)

	assert.Len(t, rset.RawRules[procFile], 1)
	assert.Equal(t, "goroutine_propagate", rset.RawRules[procFile][0].Name)

	require.Len(t, rset.FileRules, 1)
	assert.Equal(t, toolruntime.GLSFileName, rset.FileRules[0].File)
	assert.Equal(t, util.GetBuildTemp("runtime"), rset.FileRules[0].ResolvedPath)

	// Verify that runtime_gls.go was materialized to disk
	materializedFile := filepath.Join(util.GetBuildTemp("runtime"), toolruntime.GLSFileName)
	assert.True(t, util.PathExists(materializedFile))
}

func TestWarnIfNoInstrumentationMatched_RuntimeOnly(t *testing.T) {
	var logs bytes.Buffer
	sp := &setupPhase{logger: slog.New(slog.NewTextHandler(&logs, nil))}

	runtimeSet := toolruntime.BuildRuleSet("", "", t.TempDir())
	sp.warnIfNoInstrumentationMatched([]*rule.InstRuleSet{runtimeSet})

	assert.Contains(t, logs.String(), "no instrumentation rules matched any dependencies")
}

func TestWarnIfNoInstrumentationMatched_WithLibraryRules(t *testing.T) {
	var logs bytes.Buffer
	sp := &setupPhase{logger: slog.New(slog.NewTextHandler(&logs, nil))}

	runtimeSet := toolruntime.BuildRuleSet("", "", t.TempDir())
	libSet := rule.NewInstRuleSet("example.com/lib")
	libSet.AddFileRule(&rule.InstFileRule{Path: "example.com/lib/hooks", File: "hook.go"})
	sp.warnIfNoInstrumentationMatched([]*rule.InstRuleSet{runtimeSet, libSet})

	assert.NotContains(t, logs.String(), "no instrumentation rules matched any dependencies")
}

// TestRuntimeGLSDigestIsWorkDirIndependent covers builds that run the same
// setup with different --work-dir values over one GOCACHE. The runtime GLS
// source setup generates must be identical in each work dir, and the rules
// digest that feeds otelc's tool ID must not depend on where it was written,
// or no build could reuse another's cached compiles.
func TestRuntimeGLSDigestIsWorkDirIndependent(t *testing.T) {
	runtimeSrc := t.TempDir()
	deps := []*Dependency{{
		ImportPath: toolruntime.TargetRuntime,
		Sources: []string{
			filepath.Join(runtimeSrc, "runtime2.go"),
			filepath.Join(runtimeSrc, "proc.go"),
		},
	}}

	type result struct {
		source []byte
		digest string
		dir    string
	}
	setupIn := func(workDir string) result {
		t.Helper()
		t.Setenv(util.EnvOtelcWorkDir, workDir)
		sets, err := matchRuntimeGLS(deps)
		require.NoError(t, err)
		require.NotEmpty(t, sets)
		matched, err := json.Marshal(sets)
		require.NoError(t, err)
		dir := util.GetBuildTemp("runtime")
		require.Contains(t, string(matched), filepath.Base(filepath.Dir(dir)),
			"the matched rules reference the generated runtime source")
		require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), matched, 0o644))
		source, err := os.ReadFile(filepath.Join(dir, toolruntime.GLSFileName))
		require.NoError(t, err)
		return result{source: source, digest: instrument.RulesDigest(), dir: dir}
	}

	first := setupIn(t.TempDir())
	second := setupIn(t.TempDir())

	require.NotEqual(t, first.dir, second.dir)
	assert.Equal(t, first.source, second.source,
		"the generated runtime GLS source must be identical in every work dir")
	assert.NotEmpty(t, first.digest)
	assert.Equal(t, first.digest, second.digest,
		"builds in different work dirs must report the same tool ID")
}
