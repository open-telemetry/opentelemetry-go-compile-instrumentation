// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func ruleSetPaths(sets []*rule.InstRuleSet) []string {
	paths := make([]string, 0, len(sets))
	for _, rs := range sets {
		paths = append(paths, rs.ModulePath)
	}
	return paths
}

func TestRuleSetAdditions(t *testing.T) {
	dir := t.TempDir()
	withFuncRule := newTestRuleSet(
		"example.com/lib",
		[]*rule.InstFuncRule{newTestFuncRule("example.com/hook", "F")},
		nil,
	)
	withFuncRule.AddRawRule(filepath.Join(dir, "raw.go"), &rule.InstRawRule{
		InstBaseRule: rule.InstBaseRule{Imports: map[string]string{"raw": "example.com/rawimport"}},
	})
	withFuncRule.AddCallRule(filepath.Join(dir, "call.go"), &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Imports: map[string]string{"call": "example.com/callimport"}},
		ImportPath:   "example.com/called",
	})
	withFuncRule.AddLitRule(filepath.Join(dir, "lit.go"), &rule.InstLitRule{
		InstBaseRule: rule.InstBaseRule{Imports: map[string]string{"lit": "example.com/litimport"}},
		ImportPath:   "example.com/littype",
	})

	callOnly := rule.NewInstRuleSet("example.com/lib")
	callOnly.AddCallRule(filepath.Join(dir, "call.go"), &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Imports: map[string]string{"call": "example.com/callimport"}},
		ImportPath:   "example.com/called",
	})

	tests := []struct {
		name string
		rs   *rule.InstRuleSet
		want []ruleImport
	}{
		{
			name: "func rules add the imports of the runtime file",
			rs:   withFuncRule,
			want: []ruleImport{
				{path: "example.com/callimport", requires: "example.com/called"},
				{path: "example.com/hook"},
				{path: "example.com/litimport", requires: "example.com/littype"},
				{path: "example.com/rawimport"},
				{path: "log"},
				{path: "runtime/debug"},
				{path: "unsafe"},
			},
		},
		{
			name: "call rules alone do not add the imports of the runtime file",
			rs:   callOnly,
			want: []ruleImport{
				{path: "example.com/callimport", requires: "example.com/called"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, newTestSetupPhase().ruleSetAdditions(tt.rs))
		})
	}
}

func TestSelectRuleSets(t *testing.T) {
	liba := newTestRuleSet("example.com/liba", []*rule.InstFuncRule{newTestFuncRule("example.com/hooka", "A")}, nil)
	libb := newTestRuleSet("example.com/libb", []*rule.InstFuncRule{newTestFuncRule("example.com/hookb", "B")}, nil)
	// The hooks of liba import shared, which has hooks of its own.
	shared := newTestRuleSet(
		"example.com/shared",
		[]*rule.InstFuncRule{newTestFuncRule("example.com/hookshared", "S")},
		nil,
	)
	// The rules of libc add an import of extra, which has hooks of its own.
	libc := newTestRuleSet("example.com/libc", nil, nil)
	extra := newTestRuleSet(
		"example.com/extra",
		[]*rule.InstFuncRule{newTestFuncRule("example.com/hookextra", "E")},
		nil,
	)
	mainSet := newTestRuleSet(mainPackagePath, []*rule.InstFuncRule{newTestFuncRule("example.com/hookmain", "M")}, nil)

	// The rules of libd add an import of extra2 only where they find a call to
	// a function of called, which has hooks of its own.
	libd := newTestRuleSet("example.com/libd", nil, nil)
	called := newTestRuleSet(
		"example.com/called",
		[]*rule.InstFuncRule{newTestFuncRule("example.com/hookcalled", "C")},
		nil,
	)
	extra2 := newTestRuleSet(
		"example.com/extra2",
		[]*rule.InstFuncRule{newTestFuncRule("example.com/hookextra2", "E2")},
		nil,
	)

	matched := []*rule.InstRuleSet{liba, libb, shared, libc, extra, mainSet, libd, called, extra2}
	added := [][]ruleImport{
		{{path: "example.com/hooka"}},
		{{path: "example.com/hookb"}},
		{{path: "example.com/hookshared"}},
		{{path: "example.com/extra"}},
		{{path: "example.com/hookextra"}},
		{{path: "example.com/hookmain"}},
		{{path: "example.com/extra2", requires: "example.com/called"}},
		{{path: "example.com/hookcalled"}},
		{{path: "example.com/hookextra2"}},
	}
	deps := map[string]map[string]bool{
		"example.com/hooka":      {"example.com/hooka": true, "example.com/shared": true},
		"example.com/hookb":      {"example.com/hookb": true},
		"example.com/hookshared": {"example.com/hookshared": true},
		"example.com/extra":      {"example.com/extra": true},
		"example.com/hookextra":  {"example.com/hookextra": true},
		"example.com/hookmain":   {"example.com/hookmain": true},
		"example.com/extra2":     {"example.com/extra2": true},
		"example.com/hookcalled": {"example.com/hookcalled": true},
		"example.com/hookextra2": {"example.com/hookextra2": true},
	}

	tests := []struct {
		name      string
		reachable []string
		want      []string
	}{
		{
			name:      "leaves out targets the package does not build",
			reachable: []string{"example.com/app", "example.com/libb"},
			want:      []string{"example.com/libb"},
		},
		{
			name:      "follows the dependencies of selected hooks",
			reachable: []string{"example.com/app", "example.com/liba"},
			want:      []string{"example.com/liba", "example.com/shared"},
		},
		{
			name:      "follows the imports that selected rules add",
			reachable: []string{"example.com/app", "example.com/libc"},
			want:      []string{"example.com/libc", "example.com/extra"},
		},
		{
			name:      "leaves out the imports of call rules whose function the package does not build",
			reachable: []string{"example.com/app", "example.com/libd"},
			want:      []string{"example.com/libd"},
		},
		{
			name:      "follows the imports of call rules whose function the package builds",
			reachable: []string{"example.com/app", "example.com/libd", "example.com/called"},
			want:      []string{"example.com/libd", "example.com/called", "example.com/extra2"},
		},
		{
			name:      "keeps the rule sets of main packages",
			reachable: []string{"example.com/app", mainPackagePath},
			want:      []string{mainPackagePath},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reachable := make(map[string]bool)
			for _, p := range tt.reachable {
				reachable[p] = true
			}
			got := selectRuleSets(matched, added, deps, reachable)
			assert.Equal(t, tt.want, ruleSetPaths(got))
		})
	}
}

func TestRuntimeScopeRuleSets(t *testing.T) {
	lib := &packages.Package{ID: "example.com/lib", PkgPath: "example.com/lib"}
	testLib := &packages.Package{ID: "example.com/testlib", PkgPath: "example.com/testlib"}
	otherLib := &packages.Package{ID: "example.com/otherlib", PkgPath: "example.com/otherlib"}
	app := &packages.Package{
		ID:      "example.com/app",
		PkgPath: "example.com/app",
		Imports: map[string]*packages.Package{lib.PkgPath: lib},
	}
	appTest := &packages.Package{
		ID:      "example.com/app [example.com/app.test]",
		PkgPath: "example.com/app",
		ForTest: "example.com/app",
		Imports: map[string]*packages.Package{lib.PkgPath: lib, testLib.PkgPath: testLib},
	}
	otherTest := &packages.Package{
		ID:      "example.com/other [example.com/other.test]",
		PkgPath: "example.com/other",
		ForTest: "example.com/other",
		Imports: map[string]*packages.Package{otherLib.PkgPath: otherLib},
	}

	targets := []string{lib.PkgPath, testLib.PkgPath, otherLib.PkgPath}
	matched := make([]*rule.InstRuleSet, 0, len(targets))
	added := make([][]ruleImport, 0, len(targets))
	deps := make(map[string]map[string]bool)
	for _, target := range targets {
		hook := target + "/hook"
		matched = append(matched, newTestRuleSet(target, []*rule.InstFuncRule{newTestFuncRule(hook, "F")}, nil))
		added = append(added, []ruleImport{{path: hook}})
		deps[hook] = map[string]bool{hook: true}
	}
	newScope := func(testMode bool) *runtimeScope {
		return &runtimeScope{
			matched:  matched,
			added:    added,
			deps:     deps,
			testPkgs: []*packages.Package{appTest, otherTest},
			testMode: testMode,
		}
	}

	t.Run("build", func(t *testing.T) {
		got := newScope(false).ruleSets(app, app.PkgPath)
		assert.Equal(t, []string{lib.PkgPath}, ruleSetPaths(got.pkg))
		assert.Empty(t, got.test)
	})

	t.Run("test", func(t *testing.T) {
		got := newScope(true).ruleSets(app, app.PkgPath)
		assert.Equal(t, []string{lib.PkgPath}, ruleSetPaths(got.pkg), "every binary that builds app links these")
		assert.Equal(t, []string{testLib.PkgPath}, ruleSetPaths(got.test), "only the test binary of app links these")
	})
}

func TestTestPackages(t *testing.T) {
	app := &packages.Package{ID: "example.com/app", PkgPath: "example.com/app"}
	appTest := &packages.Package{
		ID:      "example.com/app [example.com/app.test]",
		PkgPath: "example.com/app",
		ForTest: "example.com/app",
	}
	appBinary := &packages.Package{ID: "example.com/app.test", PkgPath: "example.com/app.test"}

	got := testPackages([]*packages.Package{app}, []*packages.Package{app, appTest, appBinary})
	assert.Equal(t, []*packages.Package{appTest, appBinary}, got)
}

func TestNewRuntimeScopeSinglePackage(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	matched := []*rule.InstRuleSet{
		newTestRuleSet("example.com/lib", []*rule.InstFuncRule{newTestFuncRule("example.com/hook", "F")}, nil),
	}
	pkgs := []*packages.Package{{ID: "example.com/app", PkgPath: "example.com/app"}}

	scope := sp.newRuntimeScope(t.Context(), pkgs, matched, nil)
	require.Nil(t, scope, "a single build package keeps every matched rule set")
	assert.Equal(t, runtimeRuleSets{}, scope.ruleSets(pkgs[0], pkgs[0].PkgPath))
}
