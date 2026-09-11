// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package setup tests verify that the addDeps function generates
// the expected otelc.runtime.go file by comparing against golden files.
//
// To update golden files after intentional changes:
//
//	go test -update ./tool/internal/setup/...

package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
	"gotest.tools/v3/golden"
)

func TestAddDeps(t *testing.T) {
	tests := []struct {
		name        string
		matched     []*rule.InstRuleSet
		packageName string
		goldenFile  string // Empty means no file should be generated
	}{
		{
			name:        "empty_matched_rules",
			matched:     []*rule.InstRuleSet{},
			packageName: "main",
			goldenFile:  "",
		},
		{
			name: "single_func_rule",
			matched: []*rule.InstRuleSet{
				newTestRuleSet(
					"github.com/example/pkg",
					[]*rule.InstFuncRule{newTestFuncRule("github.com/example/pkg", "github.com/example/pkg")},
					nil,
				),
			},
			packageName: "main",
			goldenFile:  "single_func_rule.otelc.runtime.go.golden",
		},
		{
			name: "single_file_rule",
			matched: []*rule.InstRuleSet{
				newTestRuleSet(
					"github.com/example/pkg",
					nil,
					[]*rule.InstFileRule{newTestFileRule("github.com/example/pkg", "github.com/example/pkg")},
				),
			},
			packageName: "main",
			goldenFile:  "single_file_rule.otelc.runtime.go.golden",
		},
		{
			name: "no_rules",
			matched: []*rule.InstRuleSet{
				newTestRuleSet("github.com/example/pkg", nil, nil),
			},
			packageName: "main",
			goldenFile:  "",
		},
		{
			name: "multiple_rule_sets",
			matched: []*rule.InstRuleSet{
				newTestRuleSet(
					"github.com/example/pkg1",
					[]*rule.InstFuncRule{newTestFuncRule("github.com/example/pkg1", "github.com/example/pkg1")},
					[]*rule.InstFileRule{newTestFileRule("github.com/example/pkg2", "github.com/example/pkg2")},
				),
				newTestRuleSet(
					"github.com/example/pkg2",
					[]*rule.InstFuncRule{newTestFuncRule("github.com/example/pkg3", "github.com/example/pkg3")},
					[]*rule.InstFileRule{newTestFileRule("github.com/example/pkg4", "github.com/example/pkg4")},
				),
			},
			packageName: "main",
			goldenFile:  "multiple_rule_sets.otelc.runtime.go.golden",
		},
		{
			name: "non_main_package_name",
			matched: []*rule.InstRuleSet{
				newTestRuleSet(
					"github.com/example/pkg",
					[]*rule.InstFuncRule{newTestFuncRule("github.com/example/pkg", "github.com/example/pkg")},
					nil,
				),
			},
			packageName: "mypkg",
			goldenFile:  "non_main_package_name.otelc.runtime.go.golden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			sp := newTestSetupPhase()

			stateManager := newStateManager()
			ctx := contextWithStateManager(t.Context(), stateManager)

			err := sp.addDeps(ctx, tt.matched, tmpDir, tt.packageName, tt.packageName)
			require.NoError(t, err)

			runtimeFilePath := filepath.Join(tmpDir, otelcRuntimeFile)

			if tt.goldenFile == "" {
				assert.NoFileExists(t, runtimeFilePath)
				return
			}

			assert.FileExists(t, runtimeFilePath)
			actual, err := os.ReadFile(runtimeFilePath)
			require.NoError(t, err)

			require.Contains(t, stateManager.files, runtimeFilePath)

			actualNorm := strings.ReplaceAll(string(actual), "\r\n", "\n")
			golden.Assert(t, actualNorm, tt.goldenFile)
		})
	}
}

func TestAddDeps_FileWriteError(t *testing.T) {
	matched := []*rule.InstRuleSet{
		newTestRuleSet(
			"github.com/example/pkg",
			[]*rule.InstFuncRule{newTestFuncRule("github.com/example/pkg", "github.com/example/pkg")},
			nil,
		),
	}

	// Use a non-existent parent directory to cause write error
	invalidPath := filepath.Join(t.TempDir(), "nonexistent", "subdir")
	sp := newTestSetupPhase()

	err := sp.addDeps(t.Context(), matched, invalidPath, "main", "main")
	assert.Error(t, err)
}

func TestAddDeps_RuntimeDiffUnderDebug(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	packageDir := filepath.Join(workDir, "my_package")
	require.NoError(t, os.MkdirAll(packageDir, 0o755))

	funcRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "rule_func_http",
			Target: "example.com/target",
		},
		Path: "example.com/target",
		Func: "Handle",
	}
	fileRule := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "rule_file_helper",
			Target: "example.com/target",
		},
		Path: "example.com/target",
		File: "helper.go",
	}

	rs := rule.NewInstRuleSet("example.com/target")
	fakeFile := filepath.Join(t.TempDir(), "main.go")
	rs.AddFuncRule(fakeFile, funcRule)
	rs.AddFileRule(fileRule)

	sp := newTestSetupPhase()
	err := sp.addDeps(t.Context(), []*rule.InstRuleSet{rs}, packageDir, "main", "example.com/target")
	require.NoError(t, err)

	runtimeFilePath := filepath.Join(packageDir, otelcRuntimeFile)

	// Verify retained runtime source exists
	retainedPath := filepath.Join(setupDebugDir("example.com/target"), otelcRuntimeFile)
	assert.FileExists(t, retainedPath)

	// Verify otelc.runtime.go.diff exists next to the retained runtime source
	diffPath := filepath.Join(setupDebugDir("example.com/target"), otelcRuntimeFile+".diff")
	require.FileExists(t, diffPath)

	content, err := os.ReadFile(diffPath)
	require.NoError(t, err)
	diffText := string(content)

	// Verify header
	assert.Contains(t, diffText, "=== generated instrumentation file: otelc.runtime.go ===")
	assert.Contains(t, diffText, "rules:")
	assert.Contains(t, diffText, "  - rule_func_http")
	assert.Contains(t, diffText, "  - rule_file_helper")

	// Verify unified diff lines against /dev/null
	assert.Contains(t, diffText, "--- /dev/null")
	assert.Contains(t, diffText, "+++ "+runtimeFilePath)
	assert.Contains(t, diffText, "+package main")
	assert.Contains(t, diffText, "+import _otel_log \"log\"")
	assert.Contains(t, diffText, "+import _otel_debug \"runtime/debug\"")
}

func TestAddDeps_RuntimeContributorsAttribution(t *testing.T) {
	funcRuleA := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "rule_a",
			Target: "example.com/a",
		},
		Path: "example.com/a",
		Func: "FuncA",
	}
	funcRuleB := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "rule_b",
			Target: "example.com/b",
		},
		Path: "example.com/b",
		Func: "FuncB",
	}
	funcRuleADistinct := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "rule_a",
			Target: "example.com/a_distinct",
		},
		Path: "example.com/a_distinct",
		Func: "FuncDistinct",
	}
	fileRuleC := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "rule_c",
			Target: "example.com/c",
		},
		Path: "example.com/c",
		File: "file_c.go",
	}
	fileRuleCDup := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "rule_c",
			Target: "example.com/c",
		},
		Path: "example.com/c",
		File: "file_c.go",
	}

	funcRules := []*rule.InstFuncRule{funcRuleA, funcRuleA, funcRuleB, funcRuleADistinct}
	fileRules := []*rule.InstFileRule{fileRuleC, fileRuleCDup}

	contributors := runtimeContributors(funcRules, fileRules)
	expected := []string{"rule_a", "rule_a", "rule_b", "rule_c"}
	assert.Equal(t, expected, contributors)
}

func TestAddDeps_RuntimeDiffDebugOff(t *testing.T) {
	for _, debugVal := range []string{"", "0"} {
		t.Run("debug="+debugVal, func(t *testing.T) {
			workDir := t.TempDir()
			t.Setenv(util.EnvOtelcWorkDir, workDir)
			t.Setenv(util.EnvOtelcDebug, debugVal)

			packageDir := filepath.Join(workDir, "pkg")
			require.NoError(t, os.MkdirAll(packageDir, 0o755))

			funcRule := &rule.InstFuncRule{
				InstBaseRule: rule.InstBaseRule{
					Name:   "my_rule",
					Target: "example.com/target",
				},
				Path: "example.com/target",
				Func: "Do",
			}
			rs := rule.NewInstRuleSet("example.com/target")
			rs.AddFuncRule(filepath.Join(t.TempDir(), "f.go"), funcRule)

			sp := newTestSetupPhase()
			err := sp.addDeps(t.Context(), []*rule.InstRuleSet{rs}, packageDir, "main", "example.com/target")
			require.NoError(t, err)

			// Runtime file is generated
			runtimePath := filepath.Join(packageDir, otelcRuntimeFile)
			assert.FileExists(t, runtimePath)

			// Retained file exists via keepForDebug
			retainedPath := filepath.Join(setupDebugDir("example.com/target"), otelcRuntimeFile)
			assert.FileExists(t, retainedPath)

			// No .diff file is generated
			diffPath := filepath.Join(setupDebugDir("example.com/target"), otelcRuntimeFile+".diff")
			assert.NoFileExists(t, diffPath)
		})
	}
}

func TestAddDeps_RuntimeDiffCollision(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	// Two packages whose filesystem directories have the same basename ("client")
	cmdClientDir := filepath.Join(workDir, "cmd", "client")
	internalClientDir := filepath.Join(workDir, "internal", "client")
	require.NoError(t, os.MkdirAll(cmdClientDir, 0o755))
	require.NoError(t, os.MkdirAll(internalClientDir, 0o755))

	cmdPkgPath := "example.com/app/cmd/client"
	internalPkgPath := "example.com/app/internal/client"

	ruleCmd := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "rule_cmd", Target: "example.com/target_cmd"},
		Path:         "example.com/target_cmd",
		Func:         "DoCmd",
	}
	rsCmd := rule.NewInstRuleSet("example.com/target_cmd")
	rsCmd.AddFuncRule(filepath.Join(cmdClientDir, "client.go"), ruleCmd)

	ruleInternal := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "rule_internal", Target: "example.com/target_internal"},
		Path:         "example.com/target_internal",
		Func:         "DoInternal",
	}
	rsInternal := rule.NewInstRuleSet("example.com/target_internal")
	rsInternal.AddFuncRule(filepath.Join(internalClientDir, "client.go"), ruleInternal)

	sp := newTestSetupPhase()
	require.NoError(t, sp.addDeps(
		t.Context(), []*rule.InstRuleSet{rsCmd}, cmdClientDir, "client", cmdPkgPath))
	require.NoError(t, sp.addDeps(
		t.Context(), []*rule.InstRuleSet{rsInternal}, internalClientDir, "client", internalPkgPath))

	cmdDiffPath := filepath.Join(setupDebugDir(cmdPkgPath), otelcRuntimeFile+".diff")
	internalDiffPath := filepath.Join(setupDebugDir(internalPkgPath), otelcRuntimeFile+".diff")

	// Assert both exist independently and at expected package-escaped paths
	expectedCmdDir := filepath.Join(workDir, ".otelc-build", "debug", "example_com_app_cmd_client")
	expectedInternalDir := filepath.Join(workDir, ".otelc-build", "debug", "example_com_app_internal_client")
	assert.Equal(t, filepath.Join(expectedCmdDir, otelcRuntimeFile+".diff"), cmdDiffPath)
	assert.Equal(t, filepath.Join(expectedInternalDir, otelcRuntimeFile+".diff"), internalDiffPath)

	require.FileExists(t, cmdDiffPath)
	require.FileExists(t, internalDiffPath)

	// Assert neither overwrote the other
	cmdContent, err := os.ReadFile(cmdDiffPath)
	require.NoError(t, err)
	assert.Contains(t, string(cmdContent), "rule_cmd")
	assert.NotContains(t, string(cmdContent), "rule_internal")

	internalContent, err := os.ReadFile(internalDiffPath)
	require.NoError(t, err)
	assert.Contains(t, string(internalContent), "rule_internal")
	assert.NotContains(t, string(internalContent), "rule_cmd")

	// Also verify retained raw runtime source uses the same package-scoped directory
	cmdRetainedPath := filepath.Join(setupDebugDir(cmdPkgPath), otelcRuntimeFile)
	internalRetainedPath := filepath.Join(setupDebugDir(internalPkgPath), otelcRuntimeFile)
	assert.FileExists(t, cmdRetainedPath)
	assert.FileExists(t, internalRetainedPath)
}
