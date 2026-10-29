// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func baseRule(name string, imports map[string]string) rule.InstBaseRule {
	return rule.InstBaseRule{Name: name, Target: rule.NewTarget("**"), Imports: imports}
}

func structRuleWithImports(name string, imports map[string]string) *rule.InstStructRule {
	return &rule.InstStructRule{InstBaseRule: baseRule(name, imports), Struct: "T"}
}

// callRuleWithImports returns a rule that wraps the calls of example.com/lib.New.
func callRuleWithImports(name string, imports map[string]string) *rule.InstCallRule {
	return &rule.InstCallRule{
		InstBaseRule: baseRule(name, imports),
		FunctionCall: "example.com/lib.New",
		ImportPath:   "example.com/lib",
		FuncName:     "New",
		Replace:      "w.Wrap({{ . }})",
	}
}

// writeGoFile writes a Go source file into a temporary directory and returns
// its absolute path.
func writeGoFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.go")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestCollectRuleImports(t *testing.T) {
	set := rule.NewInstRuleSet("example.com/p")
	file := fakeSrcFile("p.go")
	other := fakeSrcFile("q.go")
	set.AddFuncRule(file, &rule.InstFuncRule{
		InstBaseRule: baseRule(
			"func",
			map[string]string{"ctx": "context", "_": "example.com/blank", ".": "example.com/dot"},
		),
	})
	set.AddStructRule(file, structRuleWithImports("struct", map[string]string{"x": "example.com/x", "_": "unsafe"}))
	set.AddCallRule(
		file,
		callRuleWithImports("call", map[string]string{"h": "example.com/h"}),
	)
	set.AddCallRule(
		other,
		callRuleWithImports("call", map[string]string{"h": "example.com/h"}),
	)
	set.AddCallRule(file, &rule.InstCallRule{
		InstBaseRule: baseRule("method", map[string]string{"m": "example.com/m"}),
		MethodCall:   "example.com/lib.*T.Do",
	})
	set.AddLitRule(file, &rule.InstLitRule{InstBaseRule: baseRule("lit", map[string]string{"l": "example.com/l"})})

	got := collectRuleImports([]*rule.InstRuleSet{set})

	require.Len(t, got, 1)
	imports := got["example.com/p"]
	type summary struct {
		rule, path string
		call       bool
		files      []string
	}
	summaries := make([]summary, 0, len(imports))
	for _, ri := range imports {
		summaries = append(summaries, summary{rule: ri.rule, path: ri.path, call: ri.call != nil, files: ri.files})
	}
	files := []string{file, other}
	if other < file {
		files = []string{other, file}
	}
	assert.Equal(t, []summary{
		{rule: "func", path: "example.com/blank"},
		{rule: "func", path: "example.com/dot"},
		{rule: "call", path: "example.com/h", call: true, files: files},
		{rule: "struct", path: "example.com/x"},
	}, summaries,
		"func rules keep only blank and dot imports, call rules group their files, "+
			"and method call rules, literal rules and unsafe are left out")
}

func TestFindRuleImportCycle(t *testing.T) {
	callsNew := writeGoFile(t, "package a\n\nimport \"example.com/lib\"\n\nvar X = lib.New()\n")
	callsOther := writeGoFile(t, "package a\n\nimport \"example.com/lib\"\n\nvar X = lib.Other()\n")
	ignoresNew := writeGoFile(t, "package a\n\nimport \"example.com/lib\"\n\n"+
		"func x() int {\n\t//otelc:ignore\n\tv := lib.New()\n\treturn v\n}\n")
	aliasesNew := writeGoFile(t, "package a\n\nimport l \"example.com/lib\"\n\nvar X = l.New()\n")

	withStruct := func(pkg, name, imported string) *rule.InstRuleSet {
		set := rule.NewInstRuleSet(pkg)
		set.AddStructRule(fakeSrcFile("s.go"), structRuleWithImports(name, map[string]string{"h": imported}))
		return set
	}
	withCall := func(file string) *rule.InstRuleSet {
		set := rule.NewInstRuleSet("example.com/a")
		set.AddCallRule(file, callRuleWithImports("wrap_new",
			map[string]string{"helper": "example.com/helper"}))
		return set
	}

	tests := []struct {
		name    string
		graph   map[string][]string
		matched []*rule.InstRuleSet
		wantErr string
	}{
		{
			name: "no cycle",
			graph: map[string][]string{
				"example.com/a":      {"example.com/lib"},
				"example.com/helper": {"example.com/lib"},
			},
			matched: []*rule.InstRuleSet{withStruct("example.com/a", "r", "example.com/helper")},
		},
		{
			name:  "direct import",
			graph: map[string][]string{"example.com/helper": {"example.com/a"}},
			matched: []*rule.InstRuleSet{
				withStruct("example.com/a", "r", "example.com/helper"),
			},
			wantErr: `rule "r" adds import "example.com/helper" to "example.com/a", ` +
				`but example.com/helper imports example.com/a`,
		},
		{
			name: "transitive import",
			graph: map[string][]string{
				"example.com/helper": {"example.com/b", "fmt"},
				"example.com/b":      {"example.com/a"},
			},
			matched: []*rule.InstRuleSet{withStruct("example.com/a", "r", "example.com/helper")},
			wantErr: `rule "r" adds import "example.com/helper" to "example.com/a", ` +
				`but example.com/helper depends on example.com/a: ` +
				`example.com/helper -> example.com/b -> example.com/a`,
		},
		{
			name: "cycle through two rules",
			graph: map[string][]string{
				"example.com/x": {"example.com/b"},
				"example.com/y": {"example.com/a"},
			},
			matched: []*rule.InstRuleSet{
				withStruct("example.com/a", "r1", "example.com/x"),
				withStruct("example.com/b", "r2", "example.com/y"),
			},
			wantErr: `rule "r2" adds import "example.com/y" to "example.com/b", ` +
				`but example.com/y depends on example.com/b: ` +
				`example.com/y -> example.com/a -> example.com/x (added by rule "r1") -> example.com/b`,
		},
		{
			name:    "import of the package itself",
			graph:   map[string][]string{"example.com/a": {"fmt"}},
			matched: []*rule.InstRuleSet{withStruct("example.com/a", "r", "example.com/a")},
			wantErr: `rule "r" adds import "example.com/a" to the package itself`,
		},
		{
			name: "call rule with a matching call",
			graph: map[string][]string{
				"example.com/a":      {"example.com/lib"},
				"example.com/helper": {"example.com/a"},
			},
			matched: []*rule.InstRuleSet{withCall(callsNew)},
			wantErr: `rule "wrap_new" adds import "example.com/helper" to "example.com/a", ` +
				`but example.com/helper imports example.com/a`,
		},
		{
			name: "call rule with an aliased matching call",
			graph: map[string][]string{
				"example.com/a":      {"example.com/lib"},
				"example.com/helper": {"example.com/a"},
			},
			matched: []*rule.InstRuleSet{withCall(aliasesNew)},
			wantErr: `rule "wrap_new" adds import "example.com/helper" to "example.com/a", ` +
				`but example.com/helper imports example.com/a`,
		},
		{
			name: "call rule in a package that does not import the function's package",
			graph: map[string][]string{
				"example.com/a":      {"fmt"},
				"example.com/helper": {"example.com/a"},
			},
			matched: []*rule.InstRuleSet{withCall(callsNew)},
		},
		{
			name: "call rule without a call to the function",
			graph: map[string][]string{
				"example.com/a":      {"example.com/lib"},
				"example.com/helper": {"example.com/a"},
			},
			matched: []*rule.InstRuleSet{withCall(callsOther)},
		},
		{
			name: "call rule whose only call is ignored",
			graph: map[string][]string{
				"example.com/a":      {"example.com/lib"},
				"example.com/helper": {"example.com/a"},
			},
			matched: []*rule.InstRuleSet{withCall(ignoresNew)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
			err := sp.findRuleImportCycle(tt.graph, collectRuleImports(tt.matched))
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestFindRuleImportCycle_UnreadableCallFile(t *testing.T) {
	set := rule.NewInstRuleSet("example.com/a")
	set.AddCallRule(fakeSrcFile("missing.go"), callRuleWithImports("wrap_new",
		map[string]string{"helper": "example.com/helper"}))
	graph := map[string][]string{
		"example.com/a":      {"example.com/lib"},
		"example.com/helper": {"example.com/a"},
	}

	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	err := sp.findRuleImportCycle(graph, collectRuleImports([]*rule.InstRuleSet{set}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing.go")
}

func TestDescribeImportCycleWithoutRuleImport(t *testing.T) {
	err := describeImportCycle([]importEdge{
		{from: "example.com/a", to: "example.com/b"},
		{from: "example.com/b", to: "example.com/a"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "import cycle: example.com/a -> example.com/b -> example.com/a")
}

// TestCheckRuleImportCycles_LoadsRuleImports covers a rule import that the
// build does not have yet: its dependencies are only known after loading it.
func TestCheckRuleImportCycles_LoadsRuleImports(t *testing.T) {
	tmpDir := t.TempDir()
	files := map[string]string{
		"go.mod":           "module example.com/app\n\ngo 1.21\n",
		"main.go":          "package main\n\nimport \"example.com/app/a\"\n\nfunc main() { println(a.X) }\n",
		"a/a.go":           "package a\n\nvar X = 1\n",
		"helper/helper.go": "package helper\n\nimport \"example.com/app/a\"\n\nfunc Wrap(v int) int { return v + a.X }\n",
		"other/other.go":   "package other\n\nfunc Wrap(v int) int { return v }\n",
	}
	for name, content := range files {
		path := filepath.Join(tmpDir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	t.Chdir(tmpDir)

	_, allPkgs, err := getBuildPackages(t.Context(), subcmdBuild, nil)
	require.NoError(t, err)

	matchedWith := func(imported string) []*rule.InstRuleSet {
		set := rule.NewInstRuleSet("example.com/app/a")
		set.AddStructRule(filepath.Join(tmpDir, "a", "a.go"),
			structRuleWithImports("add_field", map[string]string{"h": imported}))
		return []*rule.InstRuleSet{set}
	}
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}

	err = sp.checkRuleImportCycles(t.Context(), matchedWith("example.com/app/helper"), allPkgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(),
		`rule "add_field" adds import "example.com/app/helper" to "example.com/app/a", `+
			`but example.com/app/helper imports example.com/app/a`)

	require.NoError(t, sp.checkRuleImportCycles(t.Context(), matchedWith("example.com/app/other"), allPkgs))
	require.NoError(t, sp.checkRuleImportCycles(t.Context(), nil, allPkgs))
}
