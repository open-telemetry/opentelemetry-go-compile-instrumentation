// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"go.opentelemetry.io/otelc/tool/internal/compileindex"
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
	assert.True(t, compileindex.Excludes("example.com/unmatched"))
}

func TestSetupPhaseStoreIndexErrorIsNonfatal(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTemp("compile_index.json"), 0o755))
	sp := newTestSetupPhase()
	require.NoError(t, sp.store([]*rule.InstRuleSet{}))
	assert.False(t, compileindex.Excludes("example.com/unmatched"))
	data, err := os.ReadFile(util.GetMatchedRuleFile())
	require.NoError(t, err)
	assert.JSONEq(t, "[]", string(data))
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

func TestSetupPhaseStore_AllowsIgnoredCallFiles(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	other := rule.NewInstRuleSet("example.com/other")
	other.AddIgnoredCallFile(fakeSrcFile("other.go"))

	sp := newTestSetupPhase()
	err := sp.store([]*rule.InstRuleSet{other})
	require.NoError(t, err)
	assert.False(t, compileindex.Excludes("example.com/other"))
	assert.True(t, compileindex.Excludes("example.com/unmatched"))
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

	_, err := sp.storeResolvedNames()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write import names")
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
	third := hash()
	assert.NotEqual(t, second, third)

	writeFile("go.mod", "module example.com/m\n\ngo 1.24.0\n")
	assert.NotEqual(t, third, hash(), "a change of the go directive alone must change the hash")

	require.NoError(t, os.Remove(filepath.Join(modDir, "hooks", "added.go")))
	_, err := sp.injectedHash(t.Context(), matched)
	require.ErrorContains(t, err, "added.go", "a missing add_file source must fail the hash")
}

// TestStoreInjectedHashSkipsUnloadableImports covers rule imports that do not
// load from the main module: a package that does not exist, a package that
// imports one that does not exist, and a package of a module that is in the
// module graph but not in go.mod, which makes go list fail the whole load.
// None can reach a compile, so the hash leaves them out instead of failing the
// build.
func TestStoreInjectedHashSkipsUnloadableImports(t *testing.T) {
	root := t.TempDir()
	writeFile := func(name, content string) {
		t.Helper()
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	// example.com/b is in the module graph through example.com/a only.
	writeFile("b/go.mod", "module example.com/b\n\ngo 1.25.0\n")
	writeFile("b/bp/bp.go", "package bp\n")
	writeFile("a/go.mod", "module example.com/a\n\ngo 1.25.0\n\nrequire example.com/b v0.0.0\n")
	writeFile("a/a.go", "package a\n")
	writeFile("m/go.mod", "module example.com/m\n\ngo 1.25.0\n\nrequire example.com/a v0.0.0\n\n"+
		"replace example.com/a => ../a\n\nreplace example.com/b => ../b\n")
	writeFile("m/m.go", "package m\n\nimport _ \"example.com/a\"\n")
	writeFile("m/extra/extra.go", "package extra\n\nconst Tag = \"v1\"\n")
	writeFile("m/uses/uses.go", "package uses\n\nimport _ \"example.com/m/gone\"\n")
	t.Chdir(filepath.Join(root, "m"))
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, util.BuildTempDir), 0o755))

	set := rule.NewInstRuleSet("example.com/m/dep")
	set.AddCallRule(fakeSrcFile("dep.go"), &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Imports: map[string]string{
			"extra":   "example.com/m/extra",
			"missing": "example.com/m/missing",
			"uses":    "example.com/m/uses",
			"bp":      "example.com/b/bp",
		}},
	})
	matched := []*rule.InstRuleSet{set}

	sp := newTestSetupPhase()
	require.NoError(t, sp.storeInjectedHash(t.Context(), matched))
	stored, err := os.ReadFile(util.GetInjectedHashFile())
	require.NoError(t, err)
	first := string(stored)

	hash := func() string {
		t.Helper()
		h, hashErr := sp.injectedHash(t.Context(), matched)
		require.NoError(t, hashErr)
		return h
	}
	assert.Equal(t, first, hash(), "an unchanged build must keep its hash")

	writeFile("m/extra/extra.go", "package extra\n\nconst Tag = \"v2\"\n")
	second := hash()
	assert.NotEqual(t, first, second, "the imports that load must still be hashed")

	writeFile("m/missing/missing.go", "package missing\n")
	third := hash()
	assert.NotEqual(t, second, third, "an import that starts to load must change the hash")

	writeFile("m/gone/gone.go", "package gone\n")
	assert.NotEqual(t, third, hash(), "an import whose dependency starts to load must change the hash")
}

// TestLoadInjectedPackagesReturnsOtherErrors covers a load that fails for a
// reason other than a package that does not load: the error is returned.
func TestLoadInjectedPackagesReturnsOtherErrors(t *testing.T) {
	sp := newTestSetupPhase()
	sp.buildFlags = []string{"-C", filepath.Join(t.TempDir(), "missing")}

	_, _, err := sp.loadInjectedPackages(t.Context(), []string{"example.com/a", "example.com/b"})
	require.Error(t, err)
}

func TestIsLoadFailureChecksKind(t *testing.T) {
	msg := "no Go files in /tmp/p"
	assert.True(t, isLoadFailure(packages.Error{Msg: msg, Kind: packages.ListError}))
	assert.True(t, isLoadFailure(packages.Error{Msg: msg, Kind: packages.UnknownError}))
	assert.False(t, isLoadFailure(packages.Error{Msg: msg, Kind: packages.ParseError}))
	assert.False(t, isLoadFailure(packages.Error{Msg: msg, Kind: packages.TypeError}))
	assert.False(t, isLoadFailure(packages.Error{Msg: "expected 'IDENT', found 'EOF'", Kind: packages.ListError}))
}

func TestInjectedHashFailsOnBrokenImport(t *testing.T) {
	modDir := t.TempDir()
	goMod := []byte("module example.com/m\n\ngo 1.25.0\n")
	require.NoError(t, os.WriteFile(filepath.Join(modDir, "go.mod"), goMod, 0o644))
	brokenDir := filepath.Join(modDir, "broken")
	require.NoError(t, os.MkdirAll(brokenDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(brokenDir, "broken.go"), []byte("package\n"), 0o644))
	t.Chdir(modDir)

	set := rule.NewInstRuleSet("example.com/m/dep")
	set.AddCallRule(fakeSrcFile("dep.go"), &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Imports: map[string]string{"broken": "example.com/m/broken"}},
	})

	_, err := newTestSetupPhase().injectedHash(t.Context(), []*rule.InstRuleSet{set})
	require.ErrorContains(t, err, "example.com/m/broken",
		"an import that exists but does not parse must fail the hash")
}

func TestHashPackagesInvocationIndependent(t *testing.T) {
	goroot := t.TempDir()
	hashIn := func(directory, name, content string) string {
		t.Helper()
		t.Setenv(util.EnvOtelcInvocationDir, directory)
		file := filepath.Join(directory, "pkg", "hooks", name)
		mustWriteFile(t, file, content)
		pkg := &packages.Package{
			PkgPath: "example.com/hooks",
			Module:  &packages.Module{Path: "example.com/hooks", GoVersion: "1.26"},
			GoFiles: []string{file},
		}
		var result strings.Builder
		require.NoError(t, hashPackages(&result, goroot, []*packages.Package{pkg}))
		return result.String()
	}
	first := hashIn(t.TempDir(), "hook.go", "package hooks\n")
	assert.Equal(t, first, hashIn(t.TempDir(), "hook.go", "package hooks\n"))
	assert.NotEqual(t, first, hashIn(t.TempDir(), "hook.go", "package hooks\nconst Version = 2\n"))
	assert.NotEqual(t, first, hashIn(t.TempDir(), "other.go", "package hooks\n"))
}

func TestHashPackages(t *testing.T) {
	goroot := t.TempDir()
	hashOf := func(pkgs ...*packages.Package) string {
		t.Helper()
		var b strings.Builder
		require.NoError(t, hashPackages(&b, goroot, pkgs))
		return b.String()
	}

	// A versioned module in the module cache is identified by its version, so its files are not read.
	modCache := t.TempDir()
	versioned := &packages.Package{
		PkgPath: "example.com/v/p",
		Module:  &packages.Module{Path: "example.com/v", Version: "v1.2.3", Dir: modCache},
		GoFiles: []string{filepath.Join(modCache, "missing.go")},
	}
	assert.Equal(t, "example.com/v/p example.com/v@v1.2.3\n", hashOf(versioned))

	replaced := &packages.Package{
		PkgPath: "example.com/v/p",
		Module: &packages.Module{
			Path: "example.com/v", Version: "v1.2.3",
			Replace: &packages.Module{Path: "example.com/fork", Version: "v1.2.4", Dir: modCache},
		},
	}
	assert.Equal(t, "example.com/v/p example.com/fork@v1.2.4\n", hashOf(replaced))

	// Under -mod=vendor the version comes from vendor/modules.txt but Dir is empty,
	// and the vendored files can change without a version bump.
	vendorFile := filepath.Join(t.TempDir(), "vendor", "example.com", "v", "p", "p.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(vendorFile), 0o755))
	require.NoError(t, os.WriteFile(vendorFile, []byte("package p\n"), 0o644))
	vendored := &packages.Package{
		PkgPath: "example.com/v/p",
		Module:  &packages.Module{Path: "example.com/v", Version: "v1.2.3"},
		GoFiles: []string{vendorFile},
	}
	vendoredBefore := hashOf(vendored)
	assert.Contains(t, vendoredBefore, vendorFile)
	require.NoError(t, os.WriteFile(vendorFile, []byte("package p\n\nconst X = 1\n"), 0o644))
	assert.NotEqual(t, vendoredBefore, hashOf(vendored))

	// The standard library is covered by the toolchain version, so its files are not read.
	std := &packages.Package{PkgPath: "fmt", GoFiles: []string{filepath.Join(goroot, "src", "fmt", "missing.go")}}
	assert.Empty(t, hashOf(std))

	// A package with no module outside GOROOT, as from GOPATH, is hashed by content.
	gopathFile := filepath.Join(t.TempDir(), "src", "fmtx", "fmtx.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(gopathFile), 0o755))
	require.NoError(t, os.WriteFile(gopathFile, []byte("package fmtx\n"), 0o644))
	gopath := &packages.Package{PkgPath: "fmtx", GoFiles: []string{gopathFile}}
	gopathBefore := hashOf(gopath)
	assert.Contains(t, gopathBefore, gopathFile)
	assert.Equal(t, gopathBefore, hashOf(std, gopath), "the standard library must stay skipped beside it")
	require.NoError(t, os.WriteFile(gopathFile, []byte("package fmtx\n\nconst X = 1\n"), 0o644))
	assert.NotEqual(t, gopathBefore, hashOf(gopath))

	// A GOROOT that is only a prefix of the directory name does not make a package standard.
	lookalike := filepath.Join(goroot+"x", "src", "fmt", "print.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(lookalike), 0o755))
	require.NoError(t, os.WriteFile(lookalike, []byte("package fmt\n"), 0o644))
	t.Cleanup(func() { _ = os.RemoveAll(goroot + "x") })
	assert.Contains(t, hashOf(&packages.Package{PkgPath: "fmt", GoFiles: []string{lookalike}}), lookalike)

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

	// The go directive of a local module can change behavior with the same files.
	before = hashOf(local)
	local.Module.Replace.GoVersion = "1.21"
	assert.NotEqual(t, before, hashOf(local))

	// A dependency that failed to load may be missing files, so it fails the hash.
	broken := &packages.Package{PkgPath: "example.com/broken", Errors: []packages.Error{{Msg: "no Go files"}}}
	withBroken := &packages.Package{
		PkgPath: "example.com/top",
		Imports: map[string]*packages.Package{"example.com/broken": broken},
	}
	require.ErrorContains(t, hashPackages(io.Discard, goroot, []*packages.Package{withBroken}), "example.com/broken")

	// A file hashed by content must be readable.
	gone := &packages.Package{PkgPath: "example.com/gone", GoFiles: []string{filepath.Join(t.TempDir(), "gone.go")}}
	require.ErrorContains(t, hashPackages(io.Discard, goroot, []*packages.Package{gone}), "gone.go")
}
