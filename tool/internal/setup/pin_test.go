// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"errors"
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

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
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
			want: "//go:generate go run " +
				util.OtelcToolCmdRoot +
				" pin --generate",
		},
		{
			name: "prune disabled",
			opts: PinOptions{
				Prune:    false,
				Validate: false,
				Generate: &trueValue,
			},
			want: "//go:generate go run " +
				util.OtelcToolCmdRoot +
				" pin --generate --prune=false",
		},
		{
			name: "validate enabled",
			opts: PinOptions{
				Prune:    true,
				Validate: true,
				Generate: &trueValue,
			},
			want: "//go:generate go run " +
				util.OtelcToolCmdRoot +
				" pin --generate --validate",
		},
		{
			name: "prune disabled and validate enabled",
			opts: PinOptions{
				Prune:    false,
				Validate: true,
				Generate: &trueValue,
			},
			want: "//go:generate go run " +
				util.OtelcToolCmdRoot +
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
			name: "keeps existing version",
			initial: fmt.Sprintf(`module example.com/test

go 1.25

require %s %s
`, util.OtelcRoot, testVersion),
			wantModified: false,
			wantVersion:  testVersion,
		},
		{
			name: "keeps newer version",
			initial: fmt.Sprintf(`module example.com/test

go 1.25

require %s v1.99.0
`, util.OtelcRoot),
			wantModified: false,
			wantVersion:  "v1.99.0",
		},
		{
			name: "upgrades older version",
			initial: fmt.Sprintf(`module example.com/test

go 1.25

require %s v1.0.0
`, util.OtelcRoot),
			wantModified: true,
			wantVersion:  testVersion,
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
			require.NoError(t, err)
			require.Equal(t, tt.wantModified, modified)

			content, err := os.ReadFile(goModPath)
			require.NoError(t, err)

			f, err := modfile.Parse(goModPath, content, nil)
			require.NoError(t, err)

			var found bool
			for _, req := range f.Require {
				if req.Mod.Path != util.OtelcRoot {
					continue
				}

				found = true
				require.Equal(t, tt.wantVersion, req.Mod.Version)
			}

			require.True(t, found, "expected otelc require to exist")
		})
	}
}

func TestFindPinnedToolFiles(t *testing.T) {
	for _, tt := range []struct {
		name      string
		setup     func(t *testing.T, root string) map[string]bool
		want      []string
		wantError error
	}{
		{
			name: "no tool files",
			setup: func(t *testing.T, root string) map[string]bool {
				return map[string]bool{
					filepath.Join(root, "foo"): true,
					filepath.Join(root, "bar"): true,
				}
			},
		},
		{
			name: "canonical tool file",
			setup: func(t *testing.T, root string) map[string]bool {
				moduleDir := filepath.Join(root, "foo")
				require.NoError(t, os.MkdirAll(moduleDir, 0o755))
				require.NoError(t, os.WriteFile(
					filepath.Join(moduleDir, ToolFileCanonical),
					nil,
					0o644,
				))

				return map[string]bool{
					moduleDir: true,
				}
			},
			want: []string{
				filepath.Join("foo", ToolFileCanonical),
			},
		},
		{
			name: "alias tool file",
			setup: func(t *testing.T, root string) map[string]bool {
				moduleDir := filepath.Join(root, "foo")
				require.NoError(t, os.MkdirAll(moduleDir, 0o755))
				require.NoError(t, os.WriteFile(
					filepath.Join(moduleDir, ToolFileAlias),
					nil,
					0o644,
				))

				return map[string]bool{
					moduleDir: true,
				}
			},
			want: []string{
				filepath.Join("foo", ToolFileAlias),
			},
		},
		{
			name: "both tool files",
			setup: func(t *testing.T, root string) map[string]bool {
				moduleDir := filepath.Join(root, "foo")
				require.NoError(t, os.MkdirAll(moduleDir, 0o755))

				require.NoError(t, os.WriteFile(
					filepath.Join(moduleDir, ToolFileCanonical),
					nil,
					0o644,
				))
				require.NoError(t, os.WriteFile(
					filepath.Join(moduleDir, ToolFileAlias),
					nil,
					0o644,
				))

				return map[string]bool{
					moduleDir: true,
				}
			},
			wantError: ErrNotInstrumentation,
		},
		{
			name: "multiple module dirs single tool file",
			setup: func(t *testing.T, root string) map[string]bool {
				moduleDir1 := filepath.Join(root, "foo")
				require.NoError(t, os.MkdirAll(moduleDir1, 0o755))
				require.NoError(t, os.WriteFile(
					filepath.Join(moduleDir1, ToolFileCanonical),
					nil,
					0o644,
				))

				moduleDir2 := filepath.Join(root, "bar")
				require.NoError(t, os.MkdirAll(moduleDir2, 0o755))

				return map[string]bool{
					moduleDir1: true,
					moduleDir2: true,
				}
			},
			want: []string{
				filepath.Join("foo", ToolFileCanonical),
			},
		},
		{
			name: "multiple module dirs with tool files",
			setup: func(t *testing.T, root string) map[string]bool {
				moduleDir1 := filepath.Join(root, "foo")
				require.NoError(t, os.MkdirAll(moduleDir1, 0o755))
				require.NoError(t, os.WriteFile(
					filepath.Join(moduleDir1, ToolFileCanonical),
					nil,
					0o644,
				))

				moduleDir2 := filepath.Join(root, "bar")
				require.NoError(t, os.MkdirAll(moduleDir2, 0o755))
				require.NoError(t, os.WriteFile(
					filepath.Join(moduleDir2, ToolFileAlias),
					nil,
					0o644,
				))

				return map[string]bool{
					moduleDir1: true,
					moduleDir2: true,
				}
			},
			want: []string{
				filepath.Join("foo", ToolFileCanonical),
				filepath.Join("bar", ToolFileAlias),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			moduleDirs := tt.setup(t, root)

			got, findErr := findPinnedToolFiles(moduleDirs)
			if tt.wantError != nil {
				require.ErrorIs(t, findErr, tt.wantError)
				return
			}

			require.NoError(t, findErr)

			gotFiles := make([]string, 0, len(got))
			for path := range got {
				rel, relErr := filepath.Rel(root, path)
				require.NoError(t, relErr)
				gotFiles = append(gotFiles, rel)
			}

			require.ElementsMatch(t, tt.want, gotFiles)

			for _, imports := range got {
				require.Empty(t, imports)
			}
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

func TestHandleInstrumentationVisit(t *testing.T) {
	errDummy := errors.New("dummy error")

	t.Run("nil error and validate false", func(t *testing.T) {
		toolFiles := map[string]map[string]bool{
			"otel.instrumentation.go": {},
		}
		opts := PinOptions{Prune: true, Validate: false}
		v := &InstrumentationVisit{
			ImportPath: "example.com/foo",
			ToolFile:   "otel.instrumentation.go",
			Error:      nil,
		}
		recurse, err := handleInstrumentationVisit(context.Background(), toolFiles, opts, v)
		require.NoError(t, err)
		require.True(t, recurse)
		require.Empty(t, toolFiles["otel.instrumentation.go"])
	})

	t.Run("ErrNotInstrumentation with prune true", func(t *testing.T) {
		toolFiles := map[string]map[string]bool{
			"otel.instrumentation.go": {},
		}
		opts := PinOptions{Prune: true, Validate: false}
		v := &InstrumentationVisit{
			ImportPath: "example.com/foo",
			ToolFile:   "otel.instrumentation.go",
			Error:      ErrNotInstrumentation,
		}
		recurse, err := handleInstrumentationVisit(context.Background(), toolFiles, opts, v)
		require.NoError(t, err)
		require.False(t, recurse)
		require.True(t, toolFiles["otel.instrumentation.go"]["example.com/foo"])
	})

	t.Run("generic error", func(t *testing.T) {
		toolFiles := map[string]map[string]bool{
			"otel.instrumentation.go": {},
		}
		opts := PinOptions{Prune: true, Validate: false}
		v := &InstrumentationVisit{
			ImportPath: "example.com/foo",
			ToolFile:   "otel.instrumentation.go",
			Error:      errDummy,
		}
		recurse, err := handleInstrumentationVisit(context.Background(), toolFiles, opts, v)
		require.ErrorIs(t, err, errDummy)
		require.False(t, recurse)
	})

	t.Run("validate true file does not exist", func(t *testing.T) {
		toolFiles := map[string]map[string]bool{
			"otel.instrumentation.go": {},
		}
		opts := PinOptions{Prune: true, Validate: true}
		v := &InstrumentationVisit{
			ImportPath: "example.com/foo",
			ToolFile:   "otel.instrumentation.go",
			Error:      nil,
			Config: &InstrumentationConfig{
				RuleFiles: []string{"nonexistent-file.yaml"},
			},
		}
		recurse, err := handleInstrumentationVisit(context.Background(), toolFiles, opts, v)
		require.NoError(t, err)
		require.False(t, recurse)
		require.True(t, toolFiles["otel.instrumentation.go"]["example.com/foo"])
	})

	t.Run("validate true invalid yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		invalidYamlPath := filepath.Join(tmpDir, "invalid.yaml")
		require.NoError(t, os.WriteFile(invalidYamlPath, []byte("invalid: : yaml"), 0o644))

		toolFiles := map[string]map[string]bool{
			"otel.instrumentation.go": {},
		}
		opts := PinOptions{Prune: true, Validate: true}
		v := &InstrumentationVisit{
			ImportPath: "example.com/foo",
			ToolFile:   "otel.instrumentation.go",
			Error:      nil,
			Config: &InstrumentationConfig{
				RuleFiles: []string{invalidYamlPath},
			},
		}
		recurse, err := handleInstrumentationVisit(context.Background(), toolFiles, opts, v)
		require.NoError(t, err)
		require.False(t, recurse)
		require.True(t, toolFiles["otel.instrumentation.go"]["example.com/foo"])
	})
}

func TestLoadMinimalRules(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmpDir)

	// Create rulesRoot
	rulesRoot := filepath.Join(tmpDir, util.BuildTempDir, unzippedInstDir)
	require.NoError(t, os.MkdirAll(rulesRoot, 0o755))

	// Write rules yaml in a subdirectory
	subDir := filepath.Join(rulesRoot, "example.com/instrumentation/foo")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	ruleYaml := `
rule1:
  target: example.com/foo
  version: v1.2.3
`
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "otelc.yaml"), []byte(ruleYaml), 0o644))

	// Write go.mod in the same subdirectory
	goModContent := `module example.com/instrumentation/foo

go 1.25
`
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "go.mod"), []byte(goModContent), 0o644))

	rules, err := loadMinimalRules()
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Contains(t, rules, "example.com/instrumentation/foo")
	require.Equal(t, "example.com/foo", rules["example.com/instrumentation/foo"][0].Target)
	require.Equal(t, "v1.2.3", rules["example.com/instrumentation/foo"][0].VersionRange)
}

func TestCollectToolFileImports(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "otel.instrumentation.go")

	content := `package tools

import (
	_ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/cmd/otelc"
	_ "example.com/instrumentation/foo"
	_ "example.com/instrumentation/bar"
	"fmt"
)
`
	require.NoError(t, os.WriteFile(toolFile, []byte(content), 0o644))

	// case 1: no pruned imports
	imports, err := collectToolFileImports(toolFile, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{
		"example.com/instrumentation/foo": true,
		"example.com/instrumentation/bar": true,
	}, imports)

	// case 2: with pruned imports
	imports, err = collectToolFileImports(toolFile, map[string]bool{"example.com/instrumentation/foo": true})
	require.NoError(t, err)
	require.Equal(t, map[string]bool{
		"example.com/instrumentation/bar": true,
	}, imports)
}

func TestUpdateToolFile(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "otel.instrumentation.go")

	content := `package tools

import (
	_ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/cmd/otelc"
	_ "example.com/instrumentation/foo"
)
`
	require.NoError(t, os.WriteFile(toolFile, []byte(content), 0o644))

	goModContent := fmt.Sprintf(`module example.com/test

go 1.25

require %s %s
`, util.OtelcRoot, util.Version)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0o644))

	trueValue := true
	opts := PinOptions{
		Prune:    true,
		Generate: &trueValue,
	}

	err := updateToolFile(context.Background(), toolFile, nil, opts)
	require.NoError(t, err)

	updatedContent, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	require.Contains(t, string(updatedContent), "//go:generate go run github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/cmd/otelc pin --generate")
}
