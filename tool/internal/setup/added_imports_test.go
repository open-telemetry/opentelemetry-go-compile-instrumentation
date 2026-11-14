// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func baseRule(name string, imports map[string]string) rule.InstBaseRule {
	return rule.InstBaseRule{Name: name, Target: rule.NewTarget("example.com/svc"), Imports: imports}
}

func TestRuleSetAddedImportsCollectsEveryRuleKind(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	src := fakeSrcFile("a.go")
	set := rule.NewInstRuleSet("example.com/svc")
	set.AddRawRule(src, &rule.InstRawRule{InstBaseRule: baseRule("raw", map[string]string{"r": "example.com/raw"})})
	set.AddFuncRule(src, &rule.InstFuncRule{InstBaseRule: baseRule("fn", map[string]string{"f": "example.com/fn"})})
	set.AddStructRule(src, &rule.InstStructRule{InstBaseRule: baseRule("st", map[string]string{"s": "example.com/st"})})
	set.AddCallRule(src, &rule.InstCallRule{InstBaseRule: baseRule("call", map[string]string{"c": "example.com/call"})})
	set.AddLitRule(src, &rule.InstLitRule{InstBaseRule: baseRule("lit", map[string]string{"l": "example.com/lit"})})
	set.AddDirectiveRule(src, &rule.InstDirectiveRule{
		InstBaseRule: baseRule("dir", map[string]string{"d": "example.com/dir"}),
	})
	set.AddDeclRule(src, &rule.InstDeclRule{InstBaseRule: baseRule("decl", map[string]string{"_": "example.com/decl"})})

	assert.Equal(t, []ruleImport{
		{path: "example.com/call"},
		{path: "example.com/decl"},
		{path: "example.com/dir"},
		{path: "example.com/fn"},
		{path: "example.com/lit"},
		{path: "example.com/raw"},
		{path: "example.com/st"},
	}, sp.ruleSetAddedImports(set, false))
}

func TestRuleSetAddedImportsIncludesFileRuleSourceImports(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "added.go"),
		"//go:build ignore\n\npackage svc\n\nimport (\n\t\"container/list\"\n\t_ \"unsafe\"\n)\n")
	set := rule.NewInstRuleSet("example.com/svc")
	set.AddFileRule(&rule.InstFileRule{
		InstBaseRule: baseRule("file", nil),
		Path:         "example.com/filehooks",
		File:         "added.go",
		ResolvedPath: dir,
	})

	assert.Equal(t, []ruleImport{{path: "container/list"}}, sp.ruleSetAddedImports(set, false))
}

func TestRuleSetAddedImportsAddsRuntimeForIgnore(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}

	assert.Equal(
		t,
		[]ruleImport{{path: "runtime"}},
		sp.ruleSetAddedImports(rule.NewInstRuleSet("example.com/svc"), true),
	)
	assert.Empty(t, sp.ruleSetAddedImports(rule.NewInstRuleSet("runtime"), true))
	assert.Empty(t, sp.ruleSetAddedImports(rule.NewInstRuleSet("example.com/svc"), false))
}

func TestRuleSetAddedImportsDropsSelfAndC(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	set := rule.NewInstRuleSet("example.com/svc")
	set.AddRawRule(fakeSrcFile("a.go"), &rule.InstRawRule{InstBaseRule: baseRule("raw", map[string]string{
		"svc": "example.com/svc", "C": "C", "u": "unsafe", "fmt": "fmt",
	})})

	assert.Equal(t, []ruleImport{{path: "fmt"}}, sp.ruleSetAddedImports(set, false))
}

func TestRuleSetAddedImportsKeepsCallRuleRequirement(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	set := rule.NewInstRuleSet("example.com/svc")
	set.AddCallRule(fakeSrcFile("a.go"), &rule.InstCallRule{
		InstBaseRule: baseRule("call", map[string]string{"w": "example.com/wrap"}),
		ImportPath:   "example.com/client",
	})

	assert.Equal(t,
		[]ruleImport{{path: "example.com/wrap", requires: "example.com/client"}},
		sp.ruleSetAddedImports(set, false))
}

func TestAnyIgnoredCallFiles(t *testing.T) {
	plain := rule.NewInstRuleSet("example.com/a")
	ignoring := rule.NewInstRuleSet("example.com/b")
	ignoring.AddIgnoredCallFile(fakeSrcFile("b.go"))

	assert.False(t, anyIgnoredCallFiles([]*rule.InstRuleSet{plain}))
	assert.True(t, anyIgnoredCallFiles([]*rule.InstRuleSet{plain, ignoring}))
}

func TestClassifyDir(t *testing.T) {
	modcache := filepath.FromSlash("/mod")

	kind, mod := classifyDir(filepath.FromSlash("/go/src/net/http"), modcache)
	assert.Equal(t, kindInPlace, kind)
	assert.Equal(t, moduleRoot{}, mod)

	kind, mod = classifyDir(filepath.FromSlash("/app/internal/x"), modcache)
	assert.Equal(t, kindInPlace, kind)
	assert.Equal(t, moduleRoot{}, mod)

	kind, mod = classifyDir(filepath.FromSlash("/mod/github.com/!azure/sdk@v1.2.3/sub/pkg"), modcache)
	assert.Equal(t, kindModCache, kind)
	assert.Equal(t, moduleRoot{
		Path:    "github.com/Azure/sdk",
		Version: "v1.2.3",
		Dir:     filepath.FromSlash("/mod/github.com/!azure/sdk@v1.2.3"),
	}, mod)
}

func rawSet(path string, sources []string, imports map[string]string) *rule.InstRuleSet {
	set := rule.NewInstRuleSet(path)
	set.Sources = sources
	set.AddRawRule(sources[0], &rule.InstRawRule{InstBaseRule: baseRule("raw", imports)})
	return set
}

func TestImportTargetsSkipsExistingImports(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	dir := t.TempDir()
	src := filepath.Join(dir, "a.go")
	mustWriteFile(t, src, "package a\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n")
	set := rawSet("example.com/a", []string{src}, map[string]string{"fmt": "fmt", "list": "container/list"})

	targets, err := sp.importTargets([]*rule.InstRuleSet{set}, "/nonexistent-goroot", "/nonexistent-modcache")
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, &importTarget{
		ImportPath: "example.com/a",
		Name:       "a",
		Dir:        dir,
		Kind:       kindInPlace,
		Imports:    []string{"container/list"},
	}, targets[0])
}

func TestImportTargetsOmitsSetsWithNothingToAdd(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	dir := t.TempDir()
	src := filepath.Join(dir, "a.go")
	mustWriteFile(t, src, "package a\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n")
	set := rawSet("example.com/a", []string{src}, map[string]string{"fmt": "fmt"})

	targets, err := sp.importTargets([]*rule.InstRuleSet{set}, "/g", "/m")
	require.NoError(t, err)
	assert.Empty(t, targets)
}

func TestImportTargetsXTest(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	dir := t.TempDir()
	src := filepath.Join(dir, "a_test.go")
	mustWriteFile(t, src, "package a_test\n\nimport \"testing\"\n\nvar _ testing.T\n")
	set := rawSet("example.com/a_test", []string{src}, map[string]string{"list": "container/list"})
	set.IsTest = true

	targets, err := sp.importTargets([]*rule.InstRuleSet{set}, "/g", "/m")
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, "a_test", targets[0].Name)
	assert.Equal(t, dir, targets[0].Dir)
}

func TestImportTargetsFileTarget(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	mustWriteFile(t, src, "package main\n\nfunc main() {}\n")
	set := rawSet("command-line-arguments", []string{src}, map[string]string{"list": "container/list"})

	targets, err := sp.importTargets([]*rule.InstRuleSet{set}, "/g", "/m")
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, kindRuntimeFile, targets[0].Kind)
	assert.Equal(t, []string{"container/list"}, targets[0].Imports)
}

func TestImportTargetsCallRuleNeedsMatchedImport(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	dir := t.TempDir()
	src := filepath.Join(dir, "a.go")
	mustWriteFile(t, src, "package a\n\nimport \"example.com/client\"\n\nvar _ = client.X\n")
	newSet := func(importPath string) *rule.InstRuleSet {
		set := rule.NewInstRuleSet("example.com/a")
		set.Sources = []string{src}
		set.AddCallRule(src, &rule.InstCallRule{
			InstBaseRule: baseRule("call", map[string]string{"w": "example.com/wrap"}),
			ImportPath:   importPath,
		})
		return set
	}

	targets, err := sp.importTargets([]*rule.InstRuleSet{newSet("example.com/other")}, "/g", "/m")
	require.NoError(t, err)
	assert.Empty(t, targets, "the call rule cannot fire in a package that does not import its target")

	targets, err = sp.importTargets([]*rule.InstRuleSet{newSet("example.com/client")}, "/g", "/m")
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, []string{"example.com/wrap"}, targets[0].Imports)
}

func TestImportTargetsMergesVariantsOfOnePackage(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	dir := t.TempDir()
	src := filepath.Join(dir, "a.go")
	mustWriteFile(t, src, "package a\n")
	plain := rawSet("example.com/a", []string{src}, map[string]string{"list": "container/list"})
	test := rawSet("example.com/a", []string{src}, map[string]string{"ring": "container/ring"})
	test.IsTest = true

	targets, err := sp.importTargets([]*rule.InstRuleSet{plain, test}, "/g", "/m")
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, []string{"container/list", "container/ring"}, targets[0].Imports)
}

func TestImportTargetsStdGainsNonStdImport(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	goroot := t.TempDir()
	src := filepath.Join(goroot, "src", "fmt", "print.go")
	mustWriteFile(t, src, "package fmt\n")
	set := rawSet("fmt", []string{src}, map[string]string{"x": "example.com/x"})

	_, err := sp.importTargets([]*rule.InstRuleSet{set}, goroot, "/m")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fmt")
	assert.Contains(t, err.Error(), "example.com/x")
}

func TestMatchDepsKeepsSources(t *testing.T) {
	ruleFile := filepath.Join(t.TempDir(), "rules.yaml")
	mustWriteFile(
		t,
		ruleFile,
		"hook:\n  target: example.com/svc\n  func: Handler\n  before: Before\n  path: \"example.com/hooks\"\n",
	)
	src := writeGoSource(t, "svc.go", "package svc\n\nfunc Handler() {}\n")
	sp := newTestSetupPhase()
	sp.ruleConfig = ruleFile
	idx, err := sp.loadRuleIndex(t.Context(), nil)
	require.NoError(t, err)

	matched, err := sp.matchDeps(t.Context(), []*Dependency{
		{ImportPath: "example.com/svc", Sources: []string{src}, CgoFiles: map[string]string{}},
	}, idx)
	require.NoError(t, err)
	require.Len(t, matched, 1)
	assert.Equal(t, []string{src}, matched[0].Sources)
}

func TestRuleSetAddedImportsMethodCallHasNoRequirement(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	set := rule.NewInstRuleSet("example.com/svc")
	set.AddCallRule(fakeSrcFile("a.go"), &rule.InstCallRule{
		InstBaseRule: baseRule("call", map[string]string{"w": "example.com/wrap"}),
		MethodCall:   "(*example.com/client.Logger).Info",
		ImportPath:   "example.com/client",
	})

	assert.Equal(t, []ruleImport{{path: "example.com/wrap"}}, sp.ruleSetAddedImports(set, false),
		"a method call matches on the receiver type, so the file need not import its package")
}
