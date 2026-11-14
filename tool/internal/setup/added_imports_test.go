// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

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
		{path: "example.com/call"}, {path: "example.com/decl"}, {path: "example.com/dir"},
		{path: "example.com/fn"}, {path: "example.com/lit"}, {path: "example.com/raw"}, {path: "example.com/st"},
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

	assert.Equal(t, []ruleImport{{path: "runtime"}}, sp.ruleSetAddedImports(rule.NewInstRuleSet("example.com/svc"), true))
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
