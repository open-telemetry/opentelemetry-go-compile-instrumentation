// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
	toolruntime "go.opentelemetry.io/otelc/tool/internal/runtime"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestMergeRuleSetsKeepsRuntimeGLSWithRuntimeRules(t *testing.T) {
	runtimeDir := t.TempDir()
	runtime2File := filepath.Join(runtimeDir, "runtime2.go")
	procFile := filepath.Join(runtimeDir, "proc.go")
	deps := []*Dependency{
		{ImportPath: "main", Sources: []string{filepath.Join(t.TempDir(), "main.go")}},
		{ImportPath: toolruntime.TargetRuntime, Sources: []string{runtime2File, procFile}},
	}

	appSet := rule.NewInstRuleSet("main")
	appSet.AddIgnoredCallFile(deps[0].Sources[0])
	userSet := rule.NewInstRuleSet(toolruntime.TargetRuntime)
	userSet.SetPackageName(toolruntime.TargetRuntime)
	userSet.AddStructRule(runtime2File, &rule.InstStructRule{
		InstBaseRule: rule.InstBaseRule{Name: "user_field", Target: rule.NewTarget(toolruntime.TargetRuntime)},
		Struct:       "g",
		NewField:     []*rule.InstStructField{{Name: "user_field", Type: "any"}},
	})
	userSet.AddRawRule(procFile, &rule.InstRawRule{
		InstBaseRule: rule.InstBaseRule{Name: "user_goexit1", Target: rule.NewTarget(toolruntime.TargetRuntime)},
		Func:         "goexit1",
		Raw:          "_ = 1",
	})
	// matchAllDeps appends otelc's own runtime set after the matched sets.
	matched := append([]*rule.InstRuleSet{appSet, userSet}, matchRuntimeGLS(deps)...)
	require.Len(t, matched, 3)

	merged, err := mergeRuleSets(matched, deps)
	require.NoError(t, err)

	require.Len(t, merged, 2, "runtime must have one rule set")
	assert.Same(t, appSet, merged[0])
	runtimeSet := merged[1]
	assert.Equal(t, toolruntime.TargetRuntime, runtimeSet.ModulePath)
	assert.Equal(t, toolruntime.TargetRuntime, runtimeSet.PackageName)
	assert.False(t, runtimeSet.IsTest)

	require.Len(t, runtimeSet.StructRules[runtime2File], 2)
	assert.Equal(t, "user_field", runtimeSet.StructRules[runtime2File][0].Name)
	assert.Equal(t, toolruntime.StructRule().Name, runtimeSet.StructRules[runtime2File][1].Name)
	require.Len(t, runtimeSet.RawRules[procFile], 2)
	assert.Equal(t, "user_goexit1", runtimeSet.RawRules[procFile][0].Name)
	assert.Equal(t, toolruntime.RawRule().Name, runtimeSet.RawRules[procFile][1].Name)
	require.Len(t, runtimeSet.FileRules, 1)
	assert.Equal(t, toolruntime.GLSFileName, runtimeSet.FileRules[0].File)
	assert.Equal(t, util.InternalPath, runtimeSet.FileRules[0].Path)
}

func TestMergeRuleSetsMergesSetsOfOnePackage(t *testing.T) {
	dir := t.TempDir()
	libFile := filepath.Join(dir, "lib.go")
	deps := []*Dependency{{ImportPath: "example.com/lib", Sources: []string{libFile}}}
	hookRule := func(name, hookPath string) *rule.InstFuncRule {
		return &rule.InstFuncRule{
			InstBaseRule: rule.InstBaseRule{Name: name, Target: rule.NewTarget("example.com/lib")},
			Func:         "F",
			Before:       "Before",
			Path:         hookPath,
		}
	}
	shared := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{Name: "shared_file", Target: rule.NewTarget("example.com/lib")},
		File:         "shared.go",
		Path:         "example.com/shared",
	}

	// Two rule modules for one package, both with the same file rule.
	setA := rule.NewInstRuleSet("example.com/lib")
	setA.SetPackageName("lib")
	setA.AddFuncRule(libFile, hookRule("hook_a", "example.com/a"))
	setA.AddFileRule(shared)
	setB := rule.NewInstRuleSet("example.com/lib")
	setB.SetPackageName("lib")
	setB.AddFuncRule(libFile, hookRule("hook_b", "example.com/b"))
	sharedCopy := *shared
	setB.AddFileRule(&sharedCopy)

	merged, err := mergeRuleSets([]*rule.InstRuleSet{setA, setB}, deps)
	require.NoError(t, err)

	require.Len(t, merged, 1)
	funcRules := merged[0].FuncRules[libFile]
	require.Len(t, funcRules, 2)
	assert.Equal(t, "hook_a", funcRules[0].Name)
	assert.Equal(t, "hook_b", funcRules[1].Name)
	require.Len(t, merged[0].FileRules, 1, "a rule both sets have must be applied once")
	assert.Equal(t, "shared_file", merged[0].FileRules[0].Name)
	assert.Len(t, setA.FuncRules[libFile], 1, "the merged sets must not change")
}

func TestMergeRuleSetsKeepsSetsOfDifferentCompiles(t *testing.T) {
	dir := t.TempDir()
	mainA := filepath.Join(dir, "a", "main.go")
	mainB := filepath.Join(dir, "b", "main.go")
	libFile := filepath.Join(dir, "lib", "lib.go")
	libTestFile := filepath.Join(dir, "lib", "lib_test.go")
	deps := []*Dependency{
		{ImportPath: "main", Sources: []string{mainA}},
		{ImportPath: "main", Sources: []string{mainB}},
		{ImportPath: "example.com/lib", Sources: []string{libFile}},
		{ImportPath: "example.com/lib", Sources: []string{libFile, libTestFile}, IsTest: true},
	}
	withFuncRule := func(importPath, file string, isTest bool) *rule.InstRuleSet {
		set := rule.NewInstRuleSet(importPath)
		set.IsTest = isTest
		set.AddFuncRule(file, &rule.InstFuncRule{InstBaseRule: rule.InstBaseRule{Name: "hook"}, Func: "F"})
		return set
	}
	// Every main package of a build compiles with -p main, and go test
	// compiles lib with and without its tests.
	matched := []*rule.InstRuleSet{
		withFuncRule("main", mainA, false),
		withFuncRule("main", mainB, false),
		withFuncRule("example.com/lib", libFile, false),
		withFuncRule("example.com/lib", libFile, true),
	}

	merged, err := mergeRuleSets(matched, deps)
	require.NoError(t, err)

	require.Len(t, merged, len(matched))
	for i := range matched {
		assert.Same(t, matched[i], merged[i])
	}
}

func TestMergeRuleSetsLeavesSingleSetsUnchanged(t *testing.T) {
	dir := t.TempDir()
	libFile := filepath.Join(dir, "lib.go")
	runtime2File := filepath.Join(dir, "runtime2.go")
	procFile := filepath.Join(dir, "proc.go")
	deps := []*Dependency{
		{ImportPath: "example.com/lib", Sources: []string{libFile}},
		{ImportPath: toolruntime.TargetRuntime, Sources: []string{runtime2File, procFile}},
	}
	libSet := rule.NewInstRuleSet("example.com/lib")
	libSet.SetPackageName("lib")
	libSet.AddFuncRule(libFile, &rule.InstFuncRule{InstBaseRule: rule.InstBaseRule{Name: "hook"}, Func: "F"})
	matched := append([]*rule.InstRuleSet{libSet}, matchRuntimeGLS(deps)...)
	before, err := json.Marshal(matched)
	require.NoError(t, err)

	merged, err := mergeRuleSets(matched, deps)
	require.NoError(t, err)

	require.Len(t, merged, len(matched))
	for i := range matched {
		assert.Same(t, matched[i], merged[i])
	}
	after, err := json.Marshal(merged)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after))
}
