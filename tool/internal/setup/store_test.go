// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestSetupPhaseStore(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	sp := newTestSetupPhase()

	// An empty rule set resolves no paths and writes an empty JSON array to the
	// matched-rule file.
	err := sp.store([]*rule.InstRuleSet{})
	require.NoError(t, err)

	matchedFile := util.GetMatchedRuleFile()
	assert.Equal(t, filepath.Join(util.GetBuildTempDir(), "matched.json"), matchedFile)

	data, err := os.ReadFile(matchedFile)
	require.NoError(t, err)
	assert.Equal(t, "[]", string(data))
}

func TestSetupPhaseStoreCreateError(t *testing.T) {
	// Point the work dir at a location whose .otelc-build path does not exist,
	// so os.Create fails and store returns a wrapped error.
	workDir := filepath.Join(t.TempDir(), "missing")
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	sp := newTestSetupPhase()
	err := sp.store([]*rule.InstRuleSet{})
	require.Error(t, err)
}

func TestSetupPhaseStore_FailsOnMissingRuntimeSupport(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	other := rule.NewInstRuleSet("example.com/other")
	other.AddIgnoredCallFile(fakeSrcFile("other.go"))

	sp := newTestSetupPhase()
	err := sp.store([]*rule.InstRuleSet{other})
	require.Error(t, err)
	assert.Contains(t, err.Error(), ignoreDirectiveSupportModule)
}

func TestSetupPhaseStorePGOProfile(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	sp := newTestSetupPhase()
	for _, tt := range []struct{ pgo, want string }{
		{pgo: "profile.pgo", want: "profile.pgo"},
		// A later setup without a profile clears the one recorded before.
		{pgo: "off", want: ""},
	} {
		sp.pgo = tt.pgo
		require.NoError(t, sp.store([]*rule.InstRuleSet{}))
		data, err := os.ReadFile(util.GetPGOProfileFile())
		require.NoError(t, err)
		assert.Equal(t, tt.want, string(data))
	}
}

func TestSetupPhaseStorePGOProfileAmbiguous(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	sp := newTestSetupPhase()
	sp.pgo = "auto"
	for range 2 {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "default.pgo"), nil, 0o644))
		sp.buildPackages = append(sp.buildPackages, &packages.Package{
			Name:    "main",
			GoFiles: []string{filepath.Join(dir, "main.go")},
		})
	}

	require.NoError(t, sp.store([]*rule.InstRuleSet{}))
	data, err := os.ReadFile(util.GetPGOProfileFile())
	require.NoError(t, err)
	assert.Empty(t, string(data))
}

func TestSetupPhaseStorePGOProfileWriteError(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetPGOProfileFile(), 0o755))
	err := newTestSetupPhase().store([]*rule.InstRuleSet{})
	require.ErrorContains(t, err, "failed to write PGO profile")
}

func TestSetupPhaseStoreResolvedNamesWriteError(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetImportNamesFile(), 0o755))
	sp := newTestSetupPhase()
	sp.resolvedNames = map[string]string{"example.com/a": "a"}

	err := sp.store([]*rule.InstRuleSet{})
	require.ErrorContains(t, err, "failed to write import names")
}

func TestStoreResolvedNames_WriteError(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetImportNamesFile(), 0o755))

	sp := newTestSetupPhase()
	sp.resolvedNames = map[string]string{"example.com/a": "a"}

	err := sp.storeResolvedNames()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write import names")
}

func TestEnsureIgnoreDirectiveSupport(t *testing.T) {
	t.Run("no rule set uses the directive", func(t *testing.T) {
		matched := []*rule.InstRuleSet{rule.NewInstRuleSet("example.com/a")}
		require.NoError(t, ensureIgnoreDirectiveSupport(matched))
	})

	t.Run("directive used, instrumentation/runtime not loaded", func(t *testing.T) {
		other := rule.NewInstRuleSet("example.com/other")
		other.AddIgnoredCallFile(fakeSrcFile("other.go"))
		other.AddFuncRule(fakeSrcFile("a.go"), funcRuleWithPath("fn", "example.com/hooks"))

		err := ensureIgnoreDirectiveSupport([]*rule.InstRuleSet{other})
		require.Error(t, err)
		assert.Contains(t, err.Error(), ignoreDirectiveSupportModule)
		assert.Contains(t, err.Error(), util.DirectiveIgnore)
	})

	t.Run("directive used, instrumentation/runtime loaded via a func rule", func(t *testing.T) {
		other := rule.NewInstRuleSet("example.com/other")
		other.AddIgnoredCallFile(fakeSrcFile("other.go"))
		runtimeSet := rule.NewInstRuleSet("runtime")
		runtimeSet.AddFuncRule(fakeSrcFile("runtime_gls.go"), funcRuleWithPath("fn", ignoreDirectiveSupportModule))

		require.NoError(t, ensureIgnoreDirectiveSupport([]*rule.InstRuleSet{other, runtimeSet}))
	})

	t.Run("directive used, instrumentation/runtime loaded via a file rule", func(t *testing.T) {
		other := rule.NewInstRuleSet("example.com/other")
		other.AddIgnoredCallFile(fakeSrcFile("other.go"))
		runtimeSet := rule.NewInstRuleSet("runtime")
		runtimeSet.AddFileRule(fileRuleWithPath("gls_linker", ignoreDirectiveSupportModule))

		require.NoError(t, ensureIgnoreDirectiveSupport([]*rule.InstRuleSet{other, runtimeSet}))
	})

	t.Run("ModulePath alone does not satisfy the check", func(t *testing.T) {
		// Regression guard: a rule set's ModulePath is the target package
		// (e.g. "runtime" for the standard library package), not the module
		// that supplies the rule's hook code, so it must never be compared
		// against ignoreDirectiveSupportModule directly.
		other := rule.NewInstRuleSet("example.com/other")
		other.AddIgnoredCallFile(fakeSrcFile("other.go"))
		runtimeTargetOnly := rule.NewInstRuleSet(ignoreDirectiveSupportModule)

		err := ensureIgnoreDirectiveSupport([]*rule.InstRuleSet{other, runtimeTargetOnly})
		require.Error(t, err)
	})
}

func TestResolveRulePaths(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"),
		0o644,
	))

	hooksDir := filepath.Join(dir, "hooks")
	require.NoError(t, os.MkdirAll(hooksDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(hooksDir, "hook.go"),
		[]byte("package hooks\n"),
		0o644,
	))

	rs := &rule.InstRuleSet{
		FuncRules: map[string][]*rule.InstFuncRule{
			"foo": {{
				Path: "example.com/test/hooks",
			}},
		},
		FileRules: []*rule.InstFileRule{{
			Path: "example.com/test/hooks",
		}},
	}

	err := resolveRulePaths(
		t.Context(),
		[]*rule.InstRuleSet{rs},
		[]string{dir},
	)
	require.NoError(t, err)

	require.Equal(t, hooksDir, rs.AllFuncRules()[0].ResolvedPath)
	require.Equal(t, hooksDir, rs.FileRules[0].ResolvedPath)
}

func TestResolveRulePaths_MultipleDistinctPaths(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"),
		0o644,
	))

	for _, name := range []string{"hooksa", "hooksb"} {
		pkgDir := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(pkgDir, 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(pkgDir, "hook.go"),
			[]byte("package "+name+"\n"),
			0o644,
		))
	}

	rs := &rule.InstRuleSet{
		FuncRules: map[string][]*rule.InstFuncRule{
			"foo": {{Path: "example.com/test/hooksa"}},
			"bar": {{Path: "example.com/test/hooksb"}},
		},
	}

	err := resolveRulePaths(
		t.Context(),
		[]*rule.InstRuleSet{rs},
		[]string{dir},
	)
	require.NoError(t, err)

	rules := rs.AllFuncRules()
	byPath := map[string]string{}
	for _, r := range rules {
		byPath[r.Path] = r.ResolvedPath
	}
	assert.Equal(t, filepath.Join(dir, "hooksa"), byPath["example.com/test/hooksa"])
	assert.Equal(t, filepath.Join(dir, "hooksb"), byPath["example.com/test/hooksb"])
}

func TestResolveRulePaths_LoadError(t *testing.T) {
	// A module dir that doesn't exist on disk makes packages.Load itself
	// fail (not just fail to find the package), exercising the loadErr
	// branch in resolveRulePaths rather than the per-package one.
	missingDir := filepath.Join(t.TempDir(), "does-not-exist")

	rs := &rule.InstRuleSet{
		FuncRules: map[string][]*rule.InstFuncRule{
			"foo": {{Path: "example.com/test/hooks"}},
		},
	}

	err := resolveRulePaths(
		t.Context(),
		[]*rule.InstRuleSet{rs},
		[]string{missingDir},
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to resolve import path")
}

func TestResolveRulePaths_NotFound(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"),
		0o644,
	))

	rs := &rule.InstRuleSet{
		FuncRules: map[string][]*rule.InstFuncRule{
			"foo": {{
				Path: "example.com/test/doesnotexist",
			}},
		},
	}

	err := resolveRulePaths(
		t.Context(),
		[]*rule.InstRuleSet{rs},
		[]string{dir},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to resolve import path")
}

func TestResolveRulePaths_DuplicateAndUnsortedModuleDirs(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"),
		0o644,
	))

	hooksDir := filepath.Join(dir, "hooks")
	require.NoError(t, os.MkdirAll(hooksDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(hooksDir, "hook.go"),
		[]byte("package hooks\n"),
		0o644,
	))

	rs := &rule.InstRuleSet{
		FuncRules: map[string][]*rule.InstFuncRule{
			"foo": {{
				Path: "example.com/test/hooks",
			}},
		},
	}

	callerDirs := []string{dir, dir}
	callerOrig := append([]string(nil), callerDirs...)

	err := resolveRulePaths(
		t.Context(),
		[]*rule.InstRuleSet{rs},
		callerDirs,
	)
	require.NoError(t, err)
	require.Equal(t, hooksDir, rs.AllFuncRules()[0].ResolvedPath)
	assert.Equal(t, callerOrig, callerDirs, "caller slice must not be mutated")
}
