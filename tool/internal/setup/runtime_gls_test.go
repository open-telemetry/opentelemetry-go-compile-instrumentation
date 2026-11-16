// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
