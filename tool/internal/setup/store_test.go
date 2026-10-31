// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"strings"
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

func TestSetupPhaseInjectedHash(t *testing.T) {
	modDir := t.TempDir()
	writeFile := func(name, content string) {
		t.Helper()
		path := filepath.Join(modDir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	writeFile("go.mod", "module example.com/m\n\ngo 1.25.0\n")
	writeFile("extra/extra.go",
		"package extra\n\nimport \"example.com/m/extra2\"\n\nfunc Marker() string { return extra2.Tag }\n")
	writeFile("extra2/extra2.go", "package extra2\n\nconst Tag = \"v1\"\n")
	writeFile("hooks/added.go", "//go:build ignore\n\npackage hooks\n")
	t.Chdir(modDir)

	set := rule.NewInstRuleSet("example.com/m/dep")
	set.AddCallRule(fakeSrcFile("dep.go"), &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Imports: map[string]string{"extra": "example.com/m/extra"}},
	})
	set.AddFileRule(&rule.InstFileRule{File: "added.go", ResolvedPath: filepath.Join(modDir, "hooks")})
	matched := []*rule.InstRuleSet{set}

	sp := newTestSetupPhase()
	hash := func() string {
		t.Helper()
		h, err := sp.injectedHash(t.Context(), matched)
		require.NoError(t, err)
		return h
	}

	first := hash()
	assert.Equal(t, first, hash(), "an unchanged build must keep its hash")

	// extra2 is only a dependency of the package the rule imports.
	writeFile("extra2/extra2.go", "package extra2\n\nconst Tag = \"v2\"\n")
	second := hash()
	assert.NotEqual(t, first, second)

	writeFile("hooks/added.go", "//go:build ignore\n\npackage hooks\n\nvar _ = 1\n")
	assert.NotEqual(t, second, hash())
}

func TestHashPackages(t *testing.T) {
	hashOf := func(pkgs ...*packages.Package) string {
		t.Helper()
		var b strings.Builder
		require.NoError(t, hashPackages(&b, pkgs))
		return b.String()
	}

	// A versioned module is identified by its version, so its files are not read.
	versioned := &packages.Package{
		PkgPath: "example.com/v/p",
		Module:  &packages.Module{Path: "example.com/v", Version: "v1.2.3"},
		GoFiles: []string{filepath.Join(t.TempDir(), "missing.go")},
	}
	assert.Equal(t, "example.com/v/p example.com/v@v1.2.3\n", hashOf(versioned))

	replaced := &packages.Package{
		PkgPath: "example.com/v/p",
		Module: &packages.Module{
			Path: "example.com/v", Version: "v1.2.3",
			Replace: &packages.Module{Path: "example.com/fork", Version: "v1.2.4"},
		},
	}
	assert.Equal(t, "example.com/v/p example.com/fork@v1.2.4\n", hashOf(replaced))

	std := &packages.Package{PkgPath: "fmt", GoFiles: []string{filepath.Join(t.TempDir(), "missing.go")}}
	assert.Empty(t, hashOf(std))

	// A local replace has no version, so its file content is what counts.
	file := filepath.Join(t.TempDir(), "local.go")
	require.NoError(t, os.WriteFile(file, []byte("package local\n"), 0o644))
	local := &packages.Package{
		PkgPath: "example.com/local",
		Module: &packages.Module{
			Path: "example.com/local", Version: "v0.0.0",
			Replace: &packages.Module{Path: filepath.Dir(file)},
		},
		GoFiles: []string{file},
	}
	before := hashOf(local)
	require.NoError(t, os.WriteFile(file, []byte("package local\n\nconst X = 1\n"), 0o644))
	assert.NotEqual(t, before, hashOf(local))
}
