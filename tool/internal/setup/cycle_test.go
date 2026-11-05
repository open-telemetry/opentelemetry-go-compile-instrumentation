// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func baseRule(name string, imports map[string]string) rule.InstBaseRule {
	return rule.InstBaseRule{Name: name, Target: rule.NewTarget("**"), Imports: imports}
}

// selectAll returns code that selects T from each named import in imports.
func selectAll(imports map[string]string) []string {
	code := make([]string, 0, len(imports))
	for _, alias := range slices.Sorted(maps.Keys(imports)) {
		if alias != "_" && alias != "." {
			code = append(code, alias+".T")
		}
	}
	return code
}

// structRuleWithImports returns a struct rule whose new fields use each named
// import in imports.
func structRuleWithImports(name string, imports map[string]string) *rule.InstStructRule {
	r := &rule.InstStructRule{InstBaseRule: baseRule(name, imports), Struct: "T"}
	for i, typ := range selectAll(imports) {
		r.NewField = append(r.NewField, &rule.InstStructField{Name: fmt.Sprintf("F%d", i), Type: typ})
	}
	return r
}

// callRuleWithImports returns a rule that wraps the calls of example.com/lib.New
// with code that uses each named import in imports.
func callRuleWithImports(name string, imports map[string]string) *rule.InstCallRule {
	r := callRuleWithReplace(name, imports, "{{ . }}")
	for _, sel := range selectAll(imports) {
		r.Replace = strings.TrimSuffix(sel, ".T") + ".Wrap(" + r.Replace + ")"
	}
	return r
}

// callRuleWithReplace returns a rule that replaces the calls of
// example.com/lib.New with replace.
func callRuleWithReplace(name string, imports map[string]string, replace string) *rule.InstCallRule {
	return &rule.InstCallRule{
		InstBaseRule: baseRule(name, imports),
		FunctionCall: "example.com/lib.New",
		ImportPath:   "example.com/lib",
		FuncName:     "New",
		Replace:      replace,
	}
}

// litRule returns a rule that sets fields of example.com/lib.Config literals.
func litRule(name string, imports map[string]string, fields ...*rule.InstLitField) *rule.InstLitRule {
	return &rule.InstLitRule{
		InstBaseRule:  baseRule(name, imports),
		StructLiteral: "example.com/lib.Config",
		ImportPath:    "example.com/lib",
		TypeName:      "Config",
		Field:         fields,
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
		Replace:      "m.Wrap({{ . }})",
	})
	set.AddLitRule(file, litRule("lit", map[string]string{"l": "example.com/l"},
		&rule.InstLitField{Name: "A", Value: "l.Default"}))

	got := collectRuleImports([]*rule.InstRuleSet{set})

	require.Len(t, got, 1)
	imports := got["example.com/p"]
	type summary struct {
		rule, path string
		match      bool
		method     bool
		files      []string
	}
	summaries := make([]summary, 0, len(imports))
	for _, ri := range imports {
		summaries = append(summaries, summary{
			rule: ri.rule, path: ri.path, match: ri.match != nil, method: ri.method != nil, files: ri.files,
		})
	}
	files := []string{file, other}
	if other < file {
		files = []string{other, file}
	}
	assert.Equal(t, []summary{
		{rule: "func", path: "example.com/blank"},
		{rule: "func", path: "example.com/dot"},
		{rule: "call", path: "example.com/h", match: true, files: files},
		{rule: "lit", path: "example.com/l", match: true, files: []string{file}},
		{rule: "method", path: "example.com/m", method: true, files: []string{file}},
		{rule: "struct", path: "example.com/x"},
	}, summaries,
		"func rules keep only blank and dot imports, call, literal and method call rules group their files, "+
			"and unsafe is left out")
}

// TestCollectRuleImports_UnusedImports covers the imports that a rule lists
// but that its code does not use. The instrumentation does not add them.
func TestCollectRuleImports_UnusedImports(t *testing.T) {
	imports := map[string]string{
		"used":   "example.com/used",
		"unused": "example.com/unused",
		"_":      "example.com/blank",
		".":      "example.com/dot",
	}
	tests := []struct {
		name string
		add  func(set *rule.InstRuleSet, file string)
	}{
		{"struct", func(set *rule.InstRuleSet, file string) {
			set.AddStructRule(file, &rule.InstStructRule{
				InstBaseRule: baseRule("r", imports),
				NewField:     []*rule.InstStructField{{Name: "F", Type: "*used.T"}},
			})
		}},
		{"raw", func(set *rule.InstRuleSet, file string) {
			set.AddRawRule(file, &rule.InstRawRule{
				InstBaseRule: baseRule("r", imports),
				Raw:          "used.Do() // unused.Do()\n_ = \"unused.Do()\"",
			})
		}},
		{"directive", func(set *rule.InstRuleSet, file string) {
			set.AddDirectiveRule(file, &rule.InstDirectiveRule{
				InstBaseRule: baseRule("r", imports),
				Template:     "used.Wrap({{ . }})",
			})
		}},
		{"decl", func(set *rule.InstRuleSet, file string) {
			set.AddDeclRule(file, &rule.InstDeclRule{
				InstBaseRule: baseRule("r", imports),
				Wrap:         "used.Wrap({{ . }})",
			})
		}},
		{"call", func(set *rule.InstRuleSet, file string) {
			r := callRuleWithReplace("r", imports, "({{ . }})")
			r.AppendArgs = []string{"used.Option()"}
			set.AddCallRule(file, r)
		}},
		{"literal", func(set *rule.InstRuleSet, file string) {
			set.AddLitRule(file, litRule("r", imports, &rule.InstLitField{Name: "A", Wrap: "used.Wrap({{ . }})"}))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := rule.NewInstRuleSet("example.com/p")
			tt.add(set, fakeSrcFile("p.go"))

			got := collectRuleImports([]*rule.InstRuleSet{set})

			paths := make([]string, 0, len(got["example.com/p"]))
			for _, ri := range got["example.com/p"] {
				paths = append(paths, ri.path)
			}
			assert.Equal(t, []string{"example.com/blank", "example.com/dot", "example.com/used"}, paths)
		})
	}
}

func TestFindRuleImportCycle(t *testing.T) {
	callsNew := writeGoFile(t, "package a\n\nimport \"example.com/lib\"\n\nvar X = lib.New()\n")
	callsOther := writeGoFile(t, "package a\n\nimport \"example.com/lib\"\n\nvar X = lib.Other()\n")
	ignoresNew := writeGoFile(t, "package a\n\nimport \"example.com/lib\"\n\n"+
		"func x() int {\n\t//otelc:ignore\n\tv := lib.New()\n\treturn v\n}\n")
	aliasesNew := writeGoFile(t, "package a\n\nimport l \"example.com/lib\"\n\nvar X = l.New()\n")
	keyedConfig := writeGoFile(t, "package a\n\nimport \"example.com/lib\"\n\nvar X = &lib.Config{A: 1}\n")
	emptyConfig := writeGoFile(t, "package a\n\nimport l \"example.com/lib\"\n\nvar X = l.Config {}\n")
	positionalConfig := writeGoFile(t, "package a\n\nimport \"example.com/lib\"\n\nvar X = lib.Config{1}\n")
	otherType := writeGoFile(t, "package a\n\nimport \"example.com/lib\"\n\nvar X = lib.Options{A: 1}\n")

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

	withLit := func(file string, field *rule.InstLitField) *rule.InstRuleSet {
		set := rule.NewInstRuleSet("example.com/a")
		set.AddLitRule(file, litRule("set_config", map[string]string{"helper": "example.com/helper"}, field))
		return set
	}
	setsA := &rule.InstLitField{Name: "A", Wrap: "helper.Wrap({{ . }})"}
	setsB := &rule.InstLitField{Name: "B", Wrap: "helper.Wrap({{ . }})"}
	defaultsB := &rule.InstLitField{Name: "B", Value: "helper.Default"}
	litCycle := map[string][]string{
		"example.com/a":      {"example.com/lib"},
		"example.com/helper": {"example.com/a"},
	}
	litErr := `rule "set_config" adds import "example.com/helper" to "example.com/a", ` +
		`but example.com/helper imports example.com/a`

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
		{
			name: "call rule with a matching call and an unused import",
			graph: map[string][]string{
				"example.com/a":      {"example.com/lib"},
				"example.com/helper": {"example.com/a"},
			},
			matched: func() []*rule.InstRuleSet {
				set := rule.NewInstRuleSet("example.com/a")
				set.AddCallRule(callsNew, callRuleWithReplace("wrap_new",
					map[string]string{"helper": "example.com/helper"}, "({{ . }})"))
				return []*rule.InstRuleSet{set}
			}(),
		},
		{
			name:    "literal rule that wraps a field the literal sets",
			graph:   litCycle,
			matched: []*rule.InstRuleSet{withLit(keyedConfig, setsA)},
			wantErr: litErr,
		},
		{
			name:    "literal rule that sets a value in an empty literal",
			graph:   litCycle,
			matched: []*rule.InstRuleSet{withLit(emptyConfig, defaultsB)},
			wantErr: litErr,
		},
		{
			name:    "literal rule that wraps a field the literal does not set",
			graph:   litCycle,
			matched: []*rule.InstRuleSet{withLit(keyedConfig, setsB)},
		},
		{
			name:    "literal rule and a positional literal",
			graph:   litCycle,
			matched: []*rule.InstRuleSet{withLit(positionalConfig, defaultsB)},
		},
		{
			name:    "literal rule and a literal of another type",
			graph:   litCycle,
			matched: []*rule.InstRuleSet{withLit(otherType, defaultsB)},
		},
		{
			name: "literal rule in a package that does not import the type's package",
			graph: map[string][]string{
				"example.com/a":      {"fmt"},
				"example.com/helper": {"example.com/a"},
			},
			matched: []*rule.InstRuleSet{withLit(keyedConfig, setsA)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
			err := sp.findRuleImportCycle(t.Context(), tt.graph, collectRuleImports(tt.matched))
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
	err := sp.findRuleImportCycle(t.Context(), graph, collectRuleImports([]*rule.InstRuleSet{set}))
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

// TestCheckRuleImportCycles_MethodCall covers method_call rules, which need
// the types of a package to know which method a call calls.
func TestCheckRuleImportCycles_MethodCall(t *testing.T) {
	const typeCheckLog = "type-checking a package"
	tests := []struct {
		name string
		// aSource is the source of package a after its package clause.
		aSource string
		// helperImportsA makes helper import a, so the rule import closes a
		// cycle.
		helperImportsA bool
		// reachA adds a rule import of a package that imports a, so the
		// search visits a.
		reachA        bool
		wantErr       string
		wantTypeCheck bool
	}{
		{
			name:           "call of the method",
			aSource:        "import \"example.com/app/lib\"\n\nvar X = (&lib.Client{}).Do()\n",
			helperImportsA: true,
			wantErr: `rule "wrap_do" adds import "example.com/app/helper" to "example.com/app/a", ` +
				`but example.com/app/helper imports example.com/app/a`,
			wantTypeCheck: true,
		},
		{
			name: "call of the method on a value of a function result",
			aSource: "import \"example.com/app/lib\"\n\n" +
				"func get() *lib.Client { return &lib.Client{} }\n\nvar X = get().Do()\n",
			helperImportsA: true,
			wantErr:        `rule "wrap_do" adds import "example.com/app/helper" to "example.com/app/a"`,
			wantTypeCheck:  true,
		},
		{
			name:           "call of a method with the same name on another type",
			aSource:        "import \"example.com/app/lib\"\n\nvar X = (&lib.Server{}).Do()\n",
			helperImportsA: true,
			wantTypeCheck:  true,
		},
		{
			name: "ignored call of the method",
			aSource: "import \"example.com/app/lib\"\n\nvar X = get()\n\n" +
				"func get() int {\n\t//otelc:ignore\n\tv := (&lib.Client{}).Do()\n\treturn v\n}\n",
			helperImportsA: true,
		},
		{
			name:           "package that does not depend on the type's package",
			aSource:        "type T struct{}\n\nfunc (T) Do() int { return 1 }\n\nvar X = T{}.Do()\n",
			helperImportsA: true,
		},
		{
			name:    "rule import that cannot close a cycle",
			aSource: "import \"example.com/app/lib\"\n\nvar X = (&lib.Client{}).Do()\n",
			reachA:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			helper := "package helper\n\nfunc Wrap(v int) int { return v }\n"
			if tt.helperImportsA {
				helper = "package helper\n\nimport \"example.com/app/a\"\n\nfunc Wrap(v int) int { return v + a.X }\n"
			}
			files := map[string]string{
				"go.mod": "module example.com/app\n\ngo 1.21\n",
				"main.go": "package main\n\nimport (\n\t\"example.com/app/a\"\n\t\"example.com/app/helper\"\n)\n\n" +
					"func main() { println(a.X, helper.Wrap(1)) }\n",
				"lib/lib.go": "package lib\n\ntype Client struct{}\n\nfunc (*Client) Do() int { return 1 }\n\n" +
					"type Server struct{}\n\nfunc (*Server) Do() int { return 2 }\n",
				"a/a.go":           "package a\n\n" + tt.aSource,
				"helper/helper.go": helper,
				"user/user.go":     "package user\n\nimport \"example.com/app/a\"\n\nvar Y = a.X\n",
				"other/other.go":   "package other\n",
			}
			for name, content := range files {
				path := filepath.Join(tmpDir, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
			}
			t.Chdir(tmpDir)

			_, allPkgs, err := getBuildPackages(t.Context(), subcmdBuild, nil)
			require.NoError(t, err)

			set := rule.NewInstRuleSet("example.com/app/a")
			set.AddCallRule(filepath.Join(tmpDir, "a", "a.go"), &rule.InstCallRule{
				InstBaseRule: baseRule("wrap_do", map[string]string{"helper": "example.com/app/helper"}),
				MethodCall:   "example.com/app/lib.*Client.Do",
				ImportPath:   "example.com/app/lib",
				FuncName:     "Do",
				RecvType:     "*Client",
				Replace:      "helper.Wrap({{ . }})",
			})
			matched := []*rule.InstRuleSet{set}
			if tt.reachA {
				other := rule.NewInstRuleSet("example.com/app/other")
				other.AddStructRule(filepath.Join(tmpDir, "other", "other.go"),
					structRuleWithImports("add_user", map[string]string{"user": "example.com/app/user"}))
				matched = append(matched, other)
			}
			var logs strings.Builder
			sp := &setupPhase{logger: slog.New(slog.NewTextHandler(&logs, nil))}

			err = sp.checkRuleImportCycles(t.Context(), matched, allPkgs)

			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
			if tt.wantTypeCheck {
				assert.Contains(t, logs.String(), typeCheckLog)
			} else {
				assert.NotContains(t, logs.String(), typeCheckLog, "the cheap checks must skip the type check")
			}
			assert.NotContains(t, logs.String(), "skipping", "the type check must succeed")
		})
	}
}
