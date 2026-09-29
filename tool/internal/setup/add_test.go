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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"gotest.tools/v3/golden"
)

func TestAddDeps(t *testing.T) {
	tests := []struct {
		name              string
		matched           []*rule.InstRuleSet
		packageImportPath string
		packageName       string
		goldenFile        string // Empty means no file should be generated
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
		{
			name: "self_only_rules",
			matched: []*rule.InstRuleSet{
				newTestRuleSet(
					"example.com/local-hooks",
					[]*rule.InstFuncRule{newTestFuncRule("example.com/local-hooks", "example.com/app")},
					[]*rule.InstFileRule{newTestFileRule("example.com/local-hooks", "example.com/app")},
				),
			},
			packageImportPath: "example.com/local-hooks",
			packageName:       "hooks",
			goldenFile:        "",
		},
		{
			// The generated file is correct. The link still fails, because this package and
			// the instrumented package both define the same hook linkname. See
			// https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/1361
			name: "self_and_external_rules",
			matched: []*rule.InstRuleSet{
				newTestRuleSet(
					"example.com/app",
					[]*rule.InstFuncRule{
						newTestFuncRule("example.com/local-hooks", "example.com/app"),
						newTestFuncRule("example.com/external-hooks", "example.com/app"),
					},
					[]*rule.InstFileRule{
						newTestFileRule("example.com/local-hooks", "example.com/app"),
						newTestFileRule("example.com/external-file-hooks", "example.com/app"),
					},
				),
			},
			packageImportPath: "example.com/local-hooks",
			packageName:       "hooks",
			goldenFile:        "mixed_self_and_external.otelc.runtime.go.golden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			sp := newTestSetupPhase()

			stateManager := newStateManager()
			ctx := contextWithStateManager(t.Context(), stateManager)

			err := sp.addDeps(ctx, tt.matched, runtimePackage{
				dir:        tmpDir,
				importPath: tt.packageImportPath,
				name:       tt.packageName,
			})
			require.NoError(t, err)

			runtimeFilePath := filepath.Join(tmpDir, otelcRuntimeFile)

			if tt.goldenFile == "" {
				assert.NoFileExists(t, runtimeFilePath)
				assert.NotContains(t, stateManager.files, runtimeFilePath)
				return
			}

			assert.FileExists(t, runtimeFilePath)
			actual, err := os.ReadFile(runtimeFilePath)
			require.NoError(t, err)

			require.Contains(t, stateManager.files, runtimeFilePath)

			actualNorm := strings.ReplaceAll(string(actual), "\r\n", "\n")
			if tt.packageImportPath != "" {
				// Match the quoted import path, not a bare substring: a remaining
				// import for a path that merely shares tt.packageImportPath as a
				// prefix (e.g. ".../v2") must not fail this assertion.
				assert.NotContains(t, actualNorm, fmt.Sprintf("%q", tt.packageImportPath))
			}
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

	err := sp.addDeps(t.Context(), matched, runtimePackage{dir: invalidPath, name: "main"})
	assert.Error(t, err)
}

// TestAddDepsRemovesStaleRuntimeFile covers two successive setups of one package.
// The first setup generates a runtime file from an external rule. The second setup
// matches only a self-referencing rule, so addDeps must delete the runtime file.
// A stale runtime file keeps the old imports and linkname declarations active.
func TestAddDepsRemovesStaleRuntimeFile(t *testing.T) {
	tmpDir := t.TempDir()
	sp := newTestSetupPhase()

	pkg := runtimePackage{dir: tmpDir, importPath: "example.com/local-hooks", name: "hooks"}
	runtimeFilePath := filepath.Join(tmpDir, otelcRuntimeFile)

	external := []*rule.InstRuleSet{
		newTestRuleSet(
			"example.com/app",
			[]*rule.InstFuncRule{newTestFuncRule("example.com/external-hooks", "example.com/app")},
			nil,
		),
	}
	firstRunState := newStateManager()
	require.NoError(t, sp.addDeps(contextWithStateManager(t.Context(), firstRunState), external, pkg))
	require.FileExists(t, runtimeFilePath)

	selfOnly := []*rule.InstRuleSet{
		newTestRuleSet(
			"example.com/app",
			[]*rule.InstFuncRule{newTestFuncRule("example.com/local-hooks", "example.com/app")},
			nil,
		),
	}
	// A fresh state manager, as a second `otelc` run gets. Reusing firstRunState
	// would already record the path as missing, so the Track below would do
	// nothing and this test would not cover the restore path.
	secondRunState := newStateManager()
	require.NoError(t, sp.addDeps(contextWithStateManager(t.Context(), secondRunState), selfOnly, pkg))
	assert.NoFileExists(t, runtimeFilePath)

	require.NoError(t, secondRunState.Revert())
	assert.FileExists(t, runtimeFilePath)
}

// TestRemoveRuntimeFile_RemoveError covers the removal error path: os.Remove
// fails when the generated file was replaced by a non-empty directory, and
// removeRuntimeFile must surface that error instead of dropping it silently.
func TestRemoveRuntimeFile_RemoveError(t *testing.T) {
	tmpDir := t.TempDir()
	sp := newTestSetupPhase()

	runtimeFileAsDir := filepath.Join(tmpDir, otelcRuntimeFile)
	require.NoError(t, os.Mkdir(runtimeFileAsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(runtimeFileAsDir, "child"), []byte("x"), 0o600))

	err := sp.removeRuntimeFile(t.Context(), tmpDir)
	assert.Error(t, err)
}
