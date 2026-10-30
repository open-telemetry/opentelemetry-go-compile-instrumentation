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
	"go.opentelemetry.io/otelc/tool/util"
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

func TestAddDeps_RuntimeDiffUnderDebug(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcDebug, "1")

	packageDir := filepath.Join(workDir, "my_package")
	require.NoError(t, os.MkdirAll(packageDir, 0o755))

	// Keep hook path separate from target path: addDeps drops self-import rules.
	funcRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "rule_func_http",
			Target: rule.NewTarget("example.com/target"),
		},
		Path: "example.com/external-hooks",
		Func: "Handle",
	}
	fileRule := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "rule_file_helper",
			Target: rule.NewTarget("example.com/target"),
		},
		Path: "example.com/external-hooks",
		File: "helper.go",
	}

	rs := rule.NewInstRuleSet("example.com/target")
	fakeFile := filepath.Join(t.TempDir(), "main.go")
	rs.AddFuncRule(fakeFile, funcRule)
	rs.AddFileRule(fileRule)

	sp := newTestSetupPhase()
	require.NoError(t, sp.addDeps(t.Context(), []*rule.InstRuleSet{rs}, runtimePackage{
		dir:        packageDir,
		importPath: "example.com/target",
		debugPath:  "command-line-arguments",
		name:       "main",
	}))

	runtimeFilePath := filepath.Join(packageDir, otelcRuntimeFile)

	// Verify retained runtime source exists
	retainedPath := filepath.Join(setupDebugDir("command-line-arguments"), otelcRuntimeFile)
	assert.FileExists(t, retainedPath)

	// Verify otelc.runtime.go.diff exists next to the retained runtime source
	diffPath := filepath.Join(setupDebugDir("command-line-arguments"), otelcRuntimeFile+".diff")
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
			Target: rule.NewTarget("example.com/a"),
		},
		Path: "example.com/a",
		Func: "FuncA",
	}
	funcRuleB := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "rule_b",
			Target: rule.NewTarget("example.com/b"),
		},
		Path: "example.com/b",
		Func: "FuncB",
	}
	funcRuleADistinct := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "rule_a",
			Target: rule.NewTarget("example.com/a_distinct"),
		},
		Path: "example.com/a_distinct",
		Func: "FuncDistinct",
	}
	fileRuleC := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "rule_c",
			Target: rule.NewTarget("example.com/c"),
		},
		Path: "example.com/c",
		File: "file_c.go",
	}
	fileRuleCDup := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "rule_c",
			Target: rule.NewTarget("example.com/c"),
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

			// Hook lives in another package so addDeps retains this rule.
			funcRule := &rule.InstFuncRule{
				InstBaseRule: rule.InstBaseRule{
					Name:   "my_rule",
					Target: rule.NewTarget("example.com/target"),
				},
				Path: "example.com/external-hooks",
				Func: "Do",
			}
			rs := rule.NewInstRuleSet("example.com/target")
			rs.AddFuncRule(filepath.Join(t.TempDir(), "f.go"), funcRule)

			sp := newTestSetupPhase()
			err := sp.addDeps(t.Context(), []*rule.InstRuleSet{rs}, runtimePackage{
				dir:        packageDir,
				importPath: "example.com/target",
				name:       "main",
			})
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
		InstBaseRule: rule.InstBaseRule{Name: "rule_cmd", Target: rule.NewTarget("example.com/target_cmd")},
		Path:         "example.com/target_cmd",
		Func:         "DoCmd",
	}
	rsCmd := rule.NewInstRuleSet("example.com/target_cmd")
	rsCmd.AddFuncRule(filepath.Join(cmdClientDir, "client.go"), ruleCmd)

	ruleInternal := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "rule_internal", Target: rule.NewTarget("example.com/target_internal")},
		Path:         "example.com/target_internal",
		Func:         "DoInternal",
	}
	rsInternal := rule.NewInstRuleSet("example.com/target_internal")
	rsInternal.AddFuncRule(filepath.Join(internalClientDir, "client.go"), ruleInternal)

	sp := newTestSetupPhase()
	require.NoError(t, sp.addDeps(
		t.Context(), []*rule.InstRuleSet{rsCmd},
		runtimePackage{dir: cmdClientDir, importPath: cmdPkgPath, name: "client"}))
	require.NoError(t, sp.addDeps(
		t.Context(), []*rule.InstRuleSet{rsInternal},
		runtimePackage{dir: internalClientDir, importPath: internalPkgPath, name: "client"}))

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

// TestAddDepsRuntimeFileIsStable renders otelc.runtime.go repeatedly from one
// rule set and requires identical bytes. Several hook packages, each matched in
// a different source file, is the shape that exposes the order addDeps gets
// from AllFuncRules; the existing cases above share a single file key.
func TestAddDepsRuntimeFileIsStable(t *testing.T) {
	sourceDir := t.TempDir()
	hookPathByFile := map[string]string{
		"d.go": "example.com/hooks/delta",
		"b.go": "example.com/hooks/bravo",
		"a.go": "example.com/hooks/alpha",
		"c.go": "example.com/hooks/charlie",
	}

	render := func(t *testing.T) string {
		t.Helper()
		ruleSet := rule.NewInstRuleSet("example.com/app")
		for file, hookPath := range hookPathByFile {
			ruleSet.AddFuncRule(filepath.Join(sourceDir, file), newTestFuncRule(hookPath, "main"))
		}

		outDir := t.TempDir()
		err := newTestSetupPhase().addDeps(
			t.Context(),
			[]*rule.InstRuleSet{ruleSet},
			runtimePackage{dir: outDir, name: "main"},
		)
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(outDir, otelcRuntimeFile))
		require.NoError(t, err)
		return strings.ReplaceAll(string(content), "\r\n", "\n")
	}

	want := render(t)
	const iterations = 50
	for range iterations {
		require.Equal(t, want, render(t),
			"otelc.runtime.go must not change between setups of identical input")
	}
}

func TestAddDeps_FileRuleDoesNotBlankImportSourceImports(t *testing.T) {
	fileRule := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "add-hook",
			Target: rule.NewTarget("main"),
		},
		File: "injected.go",
		Path: "example.com/hooks",
	}

	matched := []*rule.InstRuleSet{
		newTestRuleSet("main", nil, []*rule.InstFileRule{fileRule}),
	}

	outDir := t.TempDir()
	sp := newTestSetupPhase()
	ctx := contextWithStateManager(t.Context(), newStateManager())

	err := sp.addDeps(ctx, matched, runtimePackage{
		dir:        outDir,
		importPath: "main",
		name:       "main",
	})
	require.NoError(t, err)

	runtimePath := filepath.Join(outDir, otelcRuntimeFile)
	require.FileExists(t, runtimePath)

	data, err := os.ReadFile(runtimePath)
	require.NoError(t, err)
	content := string(data)

	// fileRule.Path must be imported
	assert.Contains(t, content, `_ "example.com/hooks"`)
	// Source imports from injected files must NOT be blank-imported into otelc.runtime.go
	assert.NotContains(t, content, `example.com/extra`)
	assert.NotContains(t, content, `example.com/dot`)
	assert.NotContains(t, content, `example.com/blank`)
	assert.NotContains(t, content, `"context"`)
	assert.NotContains(t, content, `_ "unsafe"`)
	assert.NotContains(t, content, `_ "C"`)
}

func TestAddDeps_FileRuleSelfImportAvoidance(t *testing.T) {
	fileRule := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "self-import-rule",
			Target: rule.NewTarget("example.com/targetpkg"),
		},
		File: "hook.go",
		Path: "example.com/targetpkg",
	}

	matched := []*rule.InstRuleSet{
		newTestRuleSet("example.com/targetpkg", nil, []*rule.InstFileRule{fileRule}),
	}

	outDir := t.TempDir()
	sp := newTestSetupPhase()
	ctx := contextWithStateManager(t.Context(), newStateManager())

	err := sp.addDeps(ctx, matched, runtimePackage{
		dir:        outDir,
		importPath: "example.com/targetpkg",
		name:       "targetpkg",
	})
	require.NoError(t, err)

	runtimePath := filepath.Join(outDir, otelcRuntimeFile)
	// When all rules in matched are self-imports, no runtime file is generated
	assert.NoFileExists(t, runtimePath)
}

func TestAddDeps_FileRuleDoesNotCreateImportCycle(t *testing.T) {
	// The application dependency graph is main -> server -> config.
	// An add_file rule targets main, and the added source imports server.
	// Generating runtime files for config and server must not blank-import
	// server into config, which would create a reverse import cycle:
	// server -> config -> server.
	mainFileRule := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "main-add-file",
			Target: rule.NewTarget("example.com/app/main"),
		},
		File: "added.go",
		Path: "example.com/app/hooks",
	}

	mainFileRule.ResolvedPath = t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(mainFileRule.ResolvedPath, mainFileRule.File),
		[]byte("package main\n\nimport _ \"example.com/app/server\"\n"),
		0o600,
	))

	matched := []*rule.InstRuleSet{
		newTestRuleSet("example.com/app/main", nil, []*rule.InstFileRule{mainFileRule}),
	}

	configDir := t.TempDir()
	serverDir := t.TempDir()
	sp := newTestSetupPhase()
	ctx := contextWithStateManager(t.Context(), newStateManager())

	// Generating runtime for config (imported by server)
	err := sp.addDeps(ctx, matched, runtimePackage{
		dir:        configDir,
		importPath: "example.com/app/config",
		name:       "config",
	})
	require.NoError(t, err)

	configRuntime := filepath.Join(configDir, otelcRuntimeFile)
	require.FileExists(t, configRuntime)
	configData, err := os.ReadFile(configRuntime)
	require.NoError(t, err)
	configContent := string(configData)

	// config imports the hook package, but must NOT import server (which imports config),
	// avoiding a cycle: server -> config -> server.
	assert.Contains(t, configContent, `_ "example.com/app/hooks"`)
	assert.NotContains(t, configContent, `"example.com/app/server"`)

	// Generating runtime for server
	err = sp.addDeps(ctx, matched, runtimePackage{
		dir:        serverDir,
		importPath: "example.com/app/server",
		name:       "server",
	})
	require.NoError(t, err)

	serverRuntime := filepath.Join(serverDir, otelcRuntimeFile)
	require.FileExists(t, serverRuntime)
	data, err := os.ReadFile(serverRuntime)
	require.NoError(t, err)
	serverContent := string(data)

	// server imports hooks, but must not import itself or config
	assert.Contains(t, serverContent, `_ "example.com/app/hooks"`)
	assert.NotContains(t, serverContent, `"example.com/app/server"`)
	assert.NotContains(t, serverContent, `"example.com/app/config"`)
}

func TestAddDeps_FileRuleDoesNotViolateInternalPackageVisibility(t *testing.T) {
	// An add_file rule targets an external library (example.com/lib), and the injected
	// source file legally imports an internal package within that library
	// (example.com/lib/internal/secret). When generating otelc.runtime.go for the
	// application (example.com/app), that internal package must not be blank-imported,
	// which would violate Go's internal package visibility rules.
	fileRule := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "lib-file-rule",
			Target: rule.NewTarget("example.com/lib"),
		},
		File: "hook.go",
		Path: "example.com/lib/hook",
	}

	fileRule.ResolvedPath = t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(fileRule.ResolvedPath, fileRule.File),
		[]byte("package lib\n\nimport _ \"example.com/lib/internal/secret\"\n"),
		0o600,
	))

	matched := []*rule.InstRuleSet{
		newTestRuleSet("example.com/lib", nil, []*rule.InstFileRule{fileRule}),
	}

	appDir := t.TempDir()
	sp := newTestSetupPhase()
	ctx := contextWithStateManager(t.Context(), newStateManager())

	err := sp.addDeps(ctx, matched, runtimePackage{
		dir:        appDir,
		importPath: "example.com/app",
		name:       "app",
	})
	require.NoError(t, err)

	runtimePath := filepath.Join(appDir, otelcRuntimeFile)
	require.FileExists(t, runtimePath)

	data, err := os.ReadFile(runtimePath)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, `_ "example.com/lib/hook"`)
	assert.NotContains(t, content, `example.com/lib/internal`)
}
