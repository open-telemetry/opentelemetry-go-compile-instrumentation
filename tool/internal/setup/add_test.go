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
	"gotest.tools/v3/golden"
)

func TestAddDeps(t *testing.T) {
	tests := []struct {
		name        string
		matched     func(t *testing.T) []*rule.InstRuleSet
		packageName string
		goldenFile  string // Empty means no file should be generated
	}{
		{
			name:        "empty_matched_rules",
			matched:     func(t *testing.T) []*rule.InstRuleSet { return []*rule.InstRuleSet{} },
			packageName: "main",
			goldenFile:  "",
		},
		{
			name: "single_func_rule",
			matched: func(t *testing.T) []*rule.InstRuleSet {
				return []*rule.InstRuleSet{
					newTestRuleSet(
						"github.com/example/pkg",
						[]*rule.InstFuncRule{newTestFuncRule("github.com/example/pkg", "github.com/example/pkg")},
						nil,
					),
				}
			},
			packageName: "main",
			goldenFile:  "single_func_rule.otelc.runtime.go.golden",
		},
		{
			name: "single_file_rule",
			matched: func(t *testing.T) []*rule.InstRuleSet {
				return []*rule.InstRuleSet{
					newTestRuleSet(
						"github.com/example/pkg",
						nil,
						[]*rule.InstFileRule{newTestFileRule("github.com/example/pkg", "github.com/example/pkg")},
					),
				}
			},
			packageName: "main",
			goldenFile:  "single_file_rule.otelc.runtime.go.golden",
		},
		{
			name: "file_rule_with_source_imports",
			matched: func(t *testing.T) []*rule.InstRuleSet {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "stub.go"), []byte("package helpers\n"), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "extend.go"), []byte(`//go:build ignore

package helpers

import (
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

func Extend() string { return uuid.NewString() }
`), 0o644))

				fr := newTestFileRule("github.com/example/helpers", "main")
				fr.File = "extend.go"
				fr.ResolvedPath = dir
				return []*rule.InstRuleSet{
					newTestRuleSet("main", nil, []*rule.InstFileRule{fr}),
				}
			},
			packageName: "main",
			goldenFile:  "file_rule_with_source_imports.otelc.runtime.go.golden",
		},
		{
			name: "raw_rule_imports_only",
			matched: func(t *testing.T) []*rule.InstRuleSet {
				rs := rule.NewInstRuleSet("github.com/example/pkg")
				rs.AddRawRule(filepath.Join(t.TempDir(), "file.go"), &rule.InstRawRule{
					InstBaseRule: rule.InstBaseRule{
						Target: "github.com/example/pkg",
						Imports: map[string]string{
							"uuid": "github.com/google/uuid",
						},
					},
				})
				return []*rule.InstRuleSet{rs}
			},
			packageName: "main",
			goldenFile:  "raw_rule_imports_only.otelc.runtime.go.golden",
		},
		{
			name: "no_rules",
			matched: func(t *testing.T) []*rule.InstRuleSet {
				return []*rule.InstRuleSet{
					newTestRuleSet("github.com/example/pkg", nil, nil),
				}
			},
			packageName: "main",
			goldenFile:  "",
		},
		{
			name: "multiple_rule_sets",
			matched: func(t *testing.T) []*rule.InstRuleSet {
				return []*rule.InstRuleSet{
					newTestRuleSet(
						"github.com/example/pkg1",
						[]*rule.InstFuncRule{newTestFuncRule("github.com/example/pkg1", "github.com/example/pkg1")},
						[]*rule.InstFileRule{newTestFileRule("github.com/example/pkg2", "github.com/example/pkg2")},
					),
					newTestRuleSet(
						"github.com/example/pkg3",
						[]*rule.InstFuncRule{newTestFuncRule("github.com/example/pkg3", "github.com/example/pkg3")},
						[]*rule.InstFileRule{newTestFileRule("github.com/example/pkg4", "github.com/example/pkg4")},
					),
				}
			},
			packageName: "main",
			goldenFile:  "multiple_rule_sets.otelc.runtime.go.golden",
		},
		{
			name: "non_main_package_name",
			matched: func(t *testing.T) []*rule.InstRuleSet {
				return []*rule.InstRuleSet{
					newTestRuleSet(
						"github.com/example/pkg",
						[]*rule.InstFuncRule{newTestFuncRule("github.com/example/pkg", "github.com/example/pkg")},
						nil,
					),
				}
			},
			packageName: "mypkg",
			goldenFile:  "non_main_package_name.otelc.runtime.go.golden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			sp := newTestSetupPhase()

			stateManager := NewStateManager()
			ctx := ContextWithStateManager(t.Context(), stateManager)

			importsMap, funcRules, err := collectRuntimeImports(tt.matched(t))
			require.NoError(t, err)

			err = sp.addDeps(ctx, importsMap, funcRules, tmpDir, tt.packageName)
			require.NoError(t, err)

			runtimeFilePath := filepath.Join(tmpDir, OtelcRuntimeFile)

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

	importsMap, funcRules, err := collectRuntimeImports(matched)
	require.NoError(t, err)

	err = sp.addDeps(t.Context(), importsMap, funcRules, invalidPath, "main")
	assert.Error(t, err)
}

func TestAddDeps_MissingFileRuleSource(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stub.go"), []byte("package helpers\n"), 0o644))

	fr := newTestFileRule("github.com/example/helpers", "main")
	fr.File = "missing.go"
	fr.ResolvedPath = dir

	matched := []*rule.InstRuleSet{
		newTestRuleSet("main", nil, []*rule.InstFileRule{fr}),
	}

	_, _, err := collectRuntimeImports(matched)
	require.Error(t, err)
	assert.ErrorContains(t, err, "missing.go")
}
