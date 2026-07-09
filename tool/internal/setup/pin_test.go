// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
	"gotest.tools/v3/golden"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestRemoveImports(t *testing.T) {
	for _, tt := range []struct {
		name    string
		imports []string
		remove  map[string]bool
		want    []string
		wantErr bool
	}{
		{
			name:    "remove single import",
			imports: []string{"fmt", "os", "strings"},
			remove:  map[string]bool{"os": true},
			want:    []string{"fmt", "strings"},
		},
		{
			name:    "remove multiple imports",
			imports: []string{"fmt", "os", "strings"},
			remove:  map[string]bool{"fmt": true, "strings": true},
			want:    []string{"os"},
		},
		{
			name:    "remove none",
			imports: []string{"fmt", "os"},
			remove:  map[string]bool{"strconv": true},
			want:    []string{"fmt", "os"},
		},
		{
			name:    "remove all imports",
			imports: []string{"fmt", "os"},
			remove:  map[string]bool{"fmt": true, "os": true},
			want:    nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			specs := make([]dst.Spec, 0, len(tt.imports))
			for _, imp := range tt.imports {
				specs = append(specs, &dst.ImportSpec{
					Path: &dst.BasicLit{
						Kind:  token.STRING,
						Value: strconv.Quote(imp),
					},
				})
			}

			f := &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok:   token.IMPORT,
						Specs: specs,
					},
				},
			}

			require.NoError(t, removeImports(f, tt.remove))

			var got []string
			for _, decl := range f.Decls {
				genDecl, ok := decl.(*dst.GenDecl)
				require.True(t, ok)
				require.Equal(t, token.IMPORT, genDecl.Tok)

				for _, spec := range genDecl.Specs {
					importSpec := spec.(*dst.ImportSpec)

					path, err := strconv.Unquote(importSpec.Path.Value)
					require.NoError(t, err)

					got = append(got, path)
				}
			}

			require.ElementsMatch(t, tt.want, got)
		})
	}
}

func TestGenerateDirective(t *testing.T) {
	trueValue := true

	for _, tt := range []struct {
		name string
		opts PinOptions
		want string
	}{
		{
			name: "default",
			opts: PinOptions{
				Prune:    true,
				Validate: false,
				Generate: &trueValue,
			},
			want: "//go:generate go tool " +
				util.OtelcOldToolExe +
				" pin --generate",
		},
		{
			name: "prune disabled",
			opts: PinOptions{
				Prune:    false,
				Validate: false,
				Generate: &trueValue,
			},
			want: "//go:generate go tool " +
				util.OtelcOldToolExe +
				" pin --generate --prune=false",
		},
		{
			name: "validate enabled",
			opts: PinOptions{
				Prune:    true,
				Validate: true,
				Generate: &trueValue,
			},
			want: "//go:generate go tool " +
				util.OtelcOldToolExe +
				" pin --generate --validate",
		},
		{
			name: "prune disabled and validate enabled",
			opts: PinOptions{
				Prune:    false,
				Validate: true,
				Generate: &trueValue,
			},
			want: "//go:generate go tool " +
				util.OtelcOldToolExe +
				" pin --generate --prune=false --validate",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, generateDirective(tt.opts))
		})
	}
}

func TestUpdateGenerateDirective(t *testing.T) {
	trueValue := true
	falseValue := false

	for _, tt := range []struct {
		name     string
		initial  []string
		opts     PinOptions
		expected []string
	}{
		{
			name:    "generate nil leaves directive unchanged",
			initial: []string{"// foo", generateDirective(PinOptions{Prune: true})},
			opts: PinOptions{
				Generate: nil,
			},
			expected: []string{"// foo", generateDirective(PinOptions{Prune: true})},
		},
		{
			name:    "generate true adds directive",
			initial: []string{"// foo"},
			opts: PinOptions{
				Prune:    true,
				Generate: &trueValue,
			},
			expected: []string{
				"// foo",
				generateDirective(PinOptions{
					Prune:    true,
					Generate: &trueValue,
				}),
			},
		},
		{
			name: "generate false removes directive",
			initial: []string{
				"// foo",
				generateDirective(PinOptions{Prune: true}),
				"// bar",
			},
			opts: PinOptions{
				Generate: &falseValue,
			},
			expected: []string{
				"// foo",
				"// bar",
			},
		},
		{
			name: "generate true replaces existing directive",
			initial: []string{
				"// foo",
				generateDirective(PinOptions{Prune: true}),
				"// bar",
			},
			opts: PinOptions{
				Prune:    false,
				Validate: true,
				Generate: &trueValue,
			},
			expected: []string{
				"// foo",
				"// bar",
				generateDirective(PinOptions{
					Prune:    false,
					Validate: true,
					Generate: &trueValue,
				}),
			},
		},
		{
			name: "preserves unrelated go generate directives",
			initial: []string{
				"//go:generate stringer -type=Foo",
			},
			opts: PinOptions{
				Prune:    true,
				Generate: &trueValue,
			},
			expected: []string{
				"//go:generate stringer -type=Foo",
				generateDirective(PinOptions{
					Prune:    true,
					Generate: &trueValue,
				}),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &dst.File{}
			f.Decs.Start.Append(tt.initial...)

			updateGenerateDirective(f, tt.opts)

			require.ElementsMatch(t, tt.expected, f.Decs.Start.All())
		})
	}
}

func TestGenerateOtelInstrumentationGo(t *testing.T) {
	trueValue := true
	falseValue := false

	tests := []struct {
		name       string
		imports    map[string]bool
		opts       PinOptions
		goldenFile string
	}{
		{
			name: "default",
			imports: map[string]bool{
				"example.com/instrumentation/foo": true,
				"example.com/instrumentation/bar": true,
			},
			opts: PinOptions{
				Generate: &falseValue,
			},
			goldenFile: "default.otel.instrumentation.go.golden",
		},
		{
			name: "with generate directive",
			imports: map[string]bool{
				"example.com/instrumentation/foo": true,
				"example.com/instrumentation/bar": true,
			},
			opts: PinOptions{
				Prune:    false,
				Validate: true,
				Generate: &trueValue,
			},
			goldenFile: "generate_directive.otel.instrumentation.go.golden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			outPath := filepath.Join(tmpDir, ToolFileCanonical)

			writeErr := ast.WriteFile(outPath, generateOtelInstrumentationGo(tt.imports, tt.opts))
			require.NoError(t, writeErr)

			actual, readErr := os.ReadFile(outPath)
			require.NoError(t, readErr)

			golden.Assert(t, string(actual), tt.goldenFile)
		})
	}
}

func TestEnsureOtelcRequire(t *testing.T) {
	const testVersion = "v1.2.3"

	for _, tt := range []struct {
		name         string
		initial      string
		wantModified bool
		wantVersion  string
		wantErr      bool
	}{
		{
			name: "adds missing require",
			initial: `module example.com/test

go 1.25
`,
			wantModified: true,
			wantVersion:  testVersion,
		},
		{
			name: "adds missing tool",
			initial: fmt.Sprintf(`module example.com/test

go 1.25

require %s %s
`, util.OtelcOldRoot, testVersion),
			wantModified: true,
			wantVersion:  testVersion,
		},
		{
			name: "keeps existing version",
			initial: fmt.Sprintf(`module example.com/test

go 1.25

tool %s

require %s %s
`, util.OtelcOldToolCmdRoot, util.OtelcOldRoot, testVersion),
			wantModified: false,
			wantVersion:  testVersion,
		},
		{
			name: "keeps newer version",
			initial: fmt.Sprintf(`module example.com/test

go 1.25

tool %s

require %s v1.99.0
`, util.OtelcOldToolCmdRoot, util.OtelcOldRoot),
			wantModified: false,
			wantVersion:  "v1.99.0",
		},
		{
			name: "upgrades older version",
			initial: fmt.Sprintf(`module example.com/test

go 1.25

tool %s

require %s v1.0.0
`, util.OtelcOldToolCmdRoot, util.OtelcOldRoot),
			wantModified: true,
			wantVersion:  testVersion,
		},
		{
			name:    "invalid go.mod",
			initial: "invalid",
			wantErr: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			goModPath := filepath.Join(dir, "go.mod")

			require.NoError(t, os.WriteFile(
				goModPath,
				[]byte(tt.initial),
				0o644,
			))

			modified, err := ensureOtelcRequire(dir, testVersion)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantModified, modified)

			content, err := os.ReadFile(goModPath)
			require.NoError(t, err)

			f, err := modfile.Parse(goModPath, content, nil)
			require.NoError(t, err)

			var foundTool bool
			for _, tool := range f.Tool {
				if tool.Path != util.OtelcOldToolCmdRoot {
					continue
				}

				foundTool = true
				break
			}

			var foundRequire bool
			for _, req := range f.Require {
				if req.Mod.Path != util.OtelcOldRoot {
					continue
				}

				foundRequire = true
				require.Equal(t, tt.wantVersion, req.Mod.Version)
				break
			}

			require.True(t, foundRequire, "expected otelc require to exist")
			require.True(t, foundTool, "expected otelc tool to exist")
		})
	}
}

func TestMatchInstrumentationImports(t *testing.T) {
	for _, tt := range []struct {
		name  string
		deps  []*Dependency
		rules map[string][]yamlRule
		want  map[string]bool
	}{
		{
			name: "single match",
			deps: []*Dependency{
				{
					ImportPath: "example.com/foo",
					Version:    "v1.2.3",
				},
			},
			rules: map[string][]yamlRule{
				"example.com/instrumentation/foo": {{
					Target:       "example.com/foo",
					VersionRange: "v1.2.3",
				}},
			},
			want: map[string]bool{
				"example.com/instrumentation/foo": true,
			},
		},
		{
			name: "target mismatch",
			deps: []*Dependency{
				{
					ImportPath: "example.com/foo",
					Version:    "v1.2.3",
				},
			},
			rules: map[string][]yamlRule{
				"example.com/instrumentation/bar": {{
					Target:       "example.com/bar",
					VersionRange: "v1.2.3",
				}},
			},
			want: map[string]bool{},
		},
		{
			name: "version mismatch",
			deps: []*Dependency{
				{
					ImportPath: "example.com/foo",
					Version:    "v1.2.3",
				},
			},
			rules: map[string][]yamlRule{
				"example.com/instrumentation/foo": {{
					Target:       "example.com/foo",
					VersionRange: "v1.2.4",
				}},
			},
			want: map[string]bool{},
		},
		{
			name: "glob target",
			deps: []*Dependency{
				{
					ImportPath: "example.com/foo",
					Version:    "v1.2.3",
				},
			},
			rules: map[string][]yamlRule{
				"example.com/instrumentation/foo": {{
					Target:       "example.com/*",
					VersionRange: "v1.2.3",
				}},
			},
			want: map[string]bool{
				"example.com/instrumentation/foo": true,
			},
		},
		{
			name: "root target",
			deps: []*Dependency{
				{
					ImportPath: "example.com/foo",
				},
			},
			rules: map[string][]yamlRule{
				"example.com/instrumentation/foo": {{
					Target: rule.TargetRoot,
				}},
			},
			want: map[string]bool{
				"example.com/instrumentation/foo": true,
			},
		},
		{
			name: "multiple matches",
			deps: []*Dependency{
				{
					ImportPath: "example.com/foo",
					Version:    "v1.0.0",
				},
				{
					ImportPath: "example.com/bar",
					Version:    "v2.0.0",
				},
			},
			rules: map[string][]yamlRule{
				"example.com/instrumentation/foo": {{
					Target:       "example.com/foo",
					VersionRange: "v1.0.0",
				}},
				"example.com/instrumentation/bar": {{
					Target:       "example.com/bar",
					VersionRange: "v2.0.0",
				}},
			},
			want: map[string]bool{
				"example.com/instrumentation/foo": true,
				"example.com/instrumentation/bar": true,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := matchInstrumentationImports(tt.deps, tt.rules)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLoadMinimalRules_HappyPath(t *testing.T) {
	// root directory
	dir := t.TempDir()

	// Create sub1 submodule
	sub1 := filepath.Join(dir, "sub1")
	require.NoError(t, os.Mkdir(sub1, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub1, "go.mod"), []byte("module example.com/sub1\n"), 0o644))

	ruleContent := `
rule1:
  target: example.com/target
  version: v1.0.0
`
	require.NoError(t, os.WriteFile(filepath.Join(sub1, "otelc.yaml"), []byte(ruleContent), 0o644))

	// Create nested submodule within sub1, which should be iterated separately
	nested := filepath.Join(sub1, "nested")
	require.NoError(t, os.Mkdir(nested, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module example.com/sub1/nested\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "otelc.yaml"), []byte(`
ruleNested:
  target: example.com/nested-target
  version: v1.0.0
`), 0o644))

	rules, err := loadMinimalRules(dir)
	require.NoError(t, err)

	// make sure only 2 rules are loaded (sub1 and nested, sub1 doesn't load nested rules)
	require.Len(t, rules, 2)
	require.Contains(t, rules, "example.com/sub1")
	require.Contains(t, rules, "example.com/sub1/nested")

	require.Len(t, rules["example.com/sub1"], 1)
	require.Equal(t, "example.com/target", rules["example.com/sub1"][0].Target)
	require.Equal(t, "v1.0.0", rules["example.com/sub1"][0].VersionRange)

	require.Len(t, rules["example.com/sub1/nested"], 1)
	require.Equal(t, "example.com/nested-target", rules["example.com/sub1/nested"][0].Target)
}

func TestLoadMinimalRules_InvalidGoMod(t *testing.T) {
	dir := t.TempDir()

	sub1 := filepath.Join(dir, "sub1")
	require.NoError(t, os.Mkdir(sub1, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub1, "go.mod"), []byte("invalid"), 0o644))

	_, err := loadMinimalRules(dir)
	require.Error(t, err)
}

func TestLoadMinimalRules_InvalidRuleYAML(t *testing.T) {
	dir := t.TempDir()

	sub1 := filepath.Join(dir, "sub1")
	require.NoError(t, os.Mkdir(sub1, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub1, "go.mod"), []byte("module example.com/sub1\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sub1, "otelc.yaml"), []byte("invalid: yaml: {"), 0o644))

	_, err := loadMinimalRules(dir)
	require.Error(t, err)
}

func TestUpdateToolFile(t *testing.T) {
	trueValue := true

	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte(`module example.com/test

go 1.25
`),
		0o644,
	))

	toolFile := filepath.Join(dir, ToolFileCanonical)

	writeToolFile(t, toolFile,
		"fmt",
		"example.com/remove",
	)

	err := updateToolFile(t.Context(), toolFile,
		map[string]bool{
			"example.com/remove": true,
		},
		PinOptions{
			Prune:    true,
			Generate: &trueValue,
		},
	)
	require.NoError(t, err)

	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)

	contents := string(data)

	require.Contains(t, contents, `"fmt"`)
	require.NotContains(t, contents, `"example.com/remove"`)

	require.Contains(t, contents, generateDirective(PinOptions{
		Prune:    true,
		Generate: &trueValue,
	}))

	goMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)

	require.Contains(t, string(goMod), util.OtelcOldRoot)
	require.Contains(t, string(goMod), util.OtelcOldToolCmdRoot)
}

func TestUpdateToolFile_ParseError(t *testing.T) {
	err := updateToolFile(t.Context(),
		filepath.Join(t.TempDir(), "does-not-exist.go"),
		nil,
		PinOptions{},
	)

	require.Error(t, err)
}

func TestUpdateToolFile_EnsureRequireError(t *testing.T) {
	dir := t.TempDir()

	// valid tool file
	writeToolFile(t, filepath.Join(dir, ToolFileCanonical), "fmt")

	// intentionally invalid go.mod
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("not a go mod"),
		0o644,
	))

	err := updateToolFile(t.Context(),
		filepath.Join(dir, ToolFileCanonical),
		nil,
		PinOptions{},
	)

	require.Error(t, err)
}

func TestUpdatePinnedProjects_NoInstrumentation(t *testing.T) {
	tmp := t.TempDir()

	toolFile := writeInstrumentationModule(t, tmp, "example.com/root", false, map[string]string{
		"example.com/notinstrumentation": filepath.Join(tmp, "notinstrumentation"),
	})

	writeInstrumentationModule(
		t,
		filepath.Join(tmp, "notinstrumentation"),
		"example.com/notinstrumentation",
		false,
		nil,
	)

	_, err := updatePinnedProjects(t.Context(), []string{toolFile}, PinOptions{
		Prune: true,
	})

	require.NoError(t, err)

	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)

	require.NotContains(t, string(data), "example.com/notinstrumentation")
}

func TestUpdatePinnedProjects_ResolveError(t *testing.T) {
	tmp := t.TempDir()

	root := filepath.Join(tmp, "root")
	require.NoError(t, os.Mkdir(root, 0o755))

	require.NoError(t, os.WriteFile(
		filepath.Join(root, "go.mod"),
		fmt.Appendf(nil, `module example.com/root

go 1.25

require example.com/foo v0.0.0-00010101000000-000000000000

replace example.com/foo => %s
`, filepath.Join(tmp, "does-not-exist")),
		0o644,
	))

	writeToolFile(t,
		filepath.Join(root, ToolFileCanonical),
		"example.com/foo",
	)

	_, err := updatePinnedProjects(
		t.Context(),
		[]string{filepath.Join(root, ToolFileCanonical)},
		PinOptions{},
	)

	require.Error(t, err)
}

func TestUpdatePinnedProjects_InvalidRule(t *testing.T) {
	tmp := t.TempDir()

	toolFile := writeInstrumentationModule(
		t,
		tmp,
		"example.com/root",
		false,
		map[string]string{
			"example.com/foo": filepath.Join(tmp, "foo"),
		},
	)

	foo := filepath.Join(tmp, "foo")
	require.NoError(t, os.MkdirAll(foo, 0o755))

	require.NoError(t, os.WriteFile(
		filepath.Join(foo, "go.mod"),
		[]byte("module example.com/foo\n\ngo 1.25\n"),
		0o644,
	))

	require.NoError(t, os.WriteFile(
		filepath.Join(foo, "dummy.go"),
		[]byte("package foo\n"),
		0o644,
	))

	require.NoError(t, os.WriteFile(
		filepath.Join(foo, "invalid.otelc.yaml"),
		[]byte("invalid: yaml: {"),
		0o644,
	))

	_, err := updatePinnedProjects(
		t.Context(),
		[]string{toolFile},
		PinOptions{
			Prune:    true,
			Validate: true,
		},
	)

	require.NoError(t, err)

	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)

	require.NotContains(t, string(data), "example.com/foo")
}

func TestGeneratePinnedProjects(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, dir)
	os.MkdirAll(util.GetBuildTempDir(), 0o755) // ensure .otelc-build exists

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte(`module example.com/test

go 1.25
`),
		0o644,
	))

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "main.go"),
		[]byte(`package main

import "fmt"

func main() {
	fmt.Println("Hello, World")
}
`),
		0o644,
	))

	result, err := generatePinnedProjects(
		t.Context(),
		map[string]bool{dir: true},
		PinOptions{},
	)
	require.NoError(t, err)

	// syncDeps should have run, so PinResult should be empty.
	require.NotNil(t, result)
	require.Nil(t, result.AllDeps)

	toolFile := filepath.Join(dir, ToolFileCanonical)

	require.FileExists(t, toolFile)

	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)

	contents := string(data)

	// Just verify an import decl exists
	require.Contains(t, contents, "import (")
	require.Contains(t, contents, "_ ")

	goMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)

	// Verify tool is pinned in go.mod
	require.Contains(t, string(goMod), util.OtelcOldToolCmdRoot)
	require.Contains(t, string(goMod), util.OtelcOldRoot)
}
