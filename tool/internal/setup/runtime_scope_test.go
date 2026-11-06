// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"log/slog"
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

func TestRuntimeScopeReachable(t *testing.T) {
	lib := &packages.Package{ID: "example.com/lib", PkgPath: "example.com/lib"}
	testLib := &packages.Package{ID: "example.com/testlib", PkgPath: "example.com/testlib"}
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
	other := &packages.Package{
		ID:      "example.com/other [example.com/other.test]",
		PkgPath: "example.com/other",
		ForTest: "example.com/other",
	}

	t.Run("build", func(t *testing.T) {
		s := &runtimeScope{testPkgs: []*packages.Package{appTest, other}}
		got := s.reachable(app, app.PkgPath)
		assert.Equal(t, map[string]bool{
			"example.com/app": true,
			"example.com/lib": true,
			mainPackagePath:   true,
		}, got)
	})

	t.Run("test", func(t *testing.T) {
		s := &runtimeScope{testPkgs: []*packages.Package{appTest, other}, testMode: true}
		got := s.reachable(app, app.PkgPath)
		assert.True(
			t,
			got["example.com/testlib"],
			"the dependencies of the tests of app are built into its test binary",
		)
		assert.False(t, got["example.com/other"], "the tests of other packages are not")
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
	assert.Nil(t, scope.ruleSets(pkgs[0], pkgs[0].PkgPath))
}
