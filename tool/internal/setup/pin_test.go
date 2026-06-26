// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
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

func TestLoadMinimalRules(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmpDir)

	// Create rules directory structure: .otelc-build/instrumentation/nethttp
	instDir := filepath.Join(tmpDir, ".otelc-build", unzippedInstDir, "nethttp")
	require.NoError(t, os.MkdirAll(instDir, 0o755))

	// Write go.mod
	goModContent := "module github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp\n\ngo 1.25\n"
	require.NoError(t, os.WriteFile(filepath.Join(instDir, "go.mod"), []byte(goModContent), 0o644))

	// Write otelc.yaml
	yamlContent := `
rule1:
  target: "net/http"
  version: "v1.22.0"
`
	require.NoError(t, os.WriteFile(filepath.Join(instDir, "otelc.yaml"), []byte(yamlContent), 0o644))

	rules, err := loadMinimalRules()
	require.NoError(t, err)
	require.Len(t, rules, 1)

	nethttpRules, exists := rules["github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp"]
	require.True(t, exists)
	require.Len(t, nethttpRules, 1)
	require.Equal(t, "net/http", nethttpRules[0].Target)
	require.Equal(t, "v1.22.0", nethttpRules[0].VersionRange)
}

func TestCollectToolFileImports(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "otel.instrumentation.go")

	content := `package main

import (
	_ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/cmd/otelc"
	_ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp"
	_ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/database"
)
`
	require.NoError(t, os.WriteFile(toolFile, []byte(content), 0o644))

	// Collect all imports except the pruned ones
	pruned := map[string]bool{
		"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/database": true,
	}

	imports, err := collectToolFileImports(toolFile, pruned)
	require.NoError(t, err)
	require.Len(t, imports, 1)
	require.True(
		t,
		imports["github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp"],
	)
	require.False(t, imports["github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/cmd/otelc"])
}

func TestUpdateToolFile(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "otel.instrumentation.go")

	initialContent := `package main

import (
	_ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/cmd/otelc"
	_ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp"
)
`
	require.NoError(t, os.WriteFile(toolFile, []byte(initialContent), 0o644))

	// Write a simple go.mod
	goModContent := "module example.com/test\n\ngo 1.25\n\nrequire github.com/open-telemetry/opentelemetry-go-compile-instrumentation v0.0.0\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0o644))

	opts := PinOptions{}

	ctx := t.Context()
	err := updateToolFile(ctx, toolFile, nil, opts)
	require.NoError(t, err)

	// Verify the file was updated and still contains nethttp because pruned was nil
	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "nethttp")
}

func TestHandleInstrumentationVisit(t *testing.T) {
	ctx := t.Context()

	// 1. Visit with Error == ErrNotInstrumentation
	toolFiles := map[string]map[string]bool{
		"main.go": {},
	}
	opts := PinOptions{Prune: true}
	v := &InstrumentationVisit{
		ToolFile:   "main.go",
		ImportPath: "example.com/not-inst",
		Error:      ErrNotInstrumentation,
	}
	recurse, err := handleInstrumentationVisit(ctx, toolFiles, opts, v)
	require.NoError(t, err)
	require.False(t, recurse)
	require.True(t, toolFiles["main.go"]["example.com/not-inst"])

	// 2. Visit with general error
	generalErr := errors.New("some other error")
	v = &InstrumentationVisit{
		Error: generalErr,
	}
	_, err = handleInstrumentationVisit(ctx, toolFiles, opts, v)
	require.ErrorIs(t, err, generalErr)

	// 3. Visit with opts.Validate = true, but config rule files do not exist
	opts.Validate = true
	v = &InstrumentationVisit{
		ToolFile:   "main.go",
		ImportPath: "example.com/inst",
		Config: &InstrumentationConfig{
			RuleFiles: []string{"nonexistent.yaml"},
		},
	}
	recurse, err = handleInstrumentationVisit(ctx, toolFiles, opts, v)
	require.NoError(t, err)
	require.False(t, recurse)
	require.True(t, toolFiles["main.go"]["example.com/inst"])
}

func TestPin_Basic(t *testing.T) {
	origVersion := util.Version
	util.Version = "v0.5.0"
	defer func() {
		util.Version = origVersion
	}()

	tmpDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmpDir)
	t.Setenv("GOWORK", "off")

	cwd, err := os.Getwd()
	require.NoError(t, err)
	repoRoot := filepath.Join(cwd, "..", "..", "..")

	// Create a dummy Go module
	dummyModDir := filepath.Join(tmpDir, "dummy")
	require.NoError(t, os.MkdirAll(dummyModDir, 0o755))

	// Write go.mod with replace directive pointing to local repo
	goModContent := fmt.Sprintf("module example.com/dummy\n\ngo 1.25\n\nreplace github.com/open-telemetry/opentelemetry-go-compile-instrumentation => %s\n", filepath.ToSlash(repoRoot))
	require.NoError(t, os.WriteFile(filepath.Join(dummyModDir, "go.mod"), []byte(goModContent), 0o644))

	// Write a simple main.go
	mainContent := `package main
import "fmt"
func main() {
	fmt.Println("hello")
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dummyModDir, "main.go"), []byte(mainContent), 0o644))

	origDir, getErr := os.Getwd()
	require.NoError(t, getErr)
	defer func() {
		_ = os.Chdir(origDir)
	}()
	require.NoError(t, os.Chdir(dummyModDir))

	ctx := t.Context()
	opts := PinOptions{
		Prune:    true,
		Validate: true,
		Args:     []string{"."},
	}

	// Call Pin. It should run successfully (even if no matches, it returns successfully).
	res, err := Pin(ctx, opts)
	require.NoError(t, err)
	require.NotNil(t, res)
}

func TestAutoPin_Basic(t *testing.T) {
	origVersion := util.Version
	util.Version = "v0.5.0"
	defer func() {
		util.Version = origVersion
	}()

	tmpDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmpDir)
	t.Setenv("GOWORK", "off")

	cwd, err := os.Getwd()
	require.NoError(t, err)
	repoRoot := filepath.Join(cwd, "..", "..", "..")

	// Create a dummy Go module
	dummyModDir := filepath.Join(tmpDir, "dummy")
	require.NoError(t, os.MkdirAll(dummyModDir, 0o755))

	// Write go.mod with replace directive pointing to local repo
	goModContent := fmt.Sprintf("module example.com/dummy\n\ngo 1.25\n\nreplace github.com/open-telemetry/opentelemetry-go-compile-instrumentation => %s\n", filepath.ToSlash(repoRoot))
	require.NoError(t, os.WriteFile(filepath.Join(dummyModDir, "go.mod"), []byte(goModContent), 0o644))

	// Write a simple main.go
	mainContent := `package main
import "fmt"
func main() {
	fmt.Println("hello")
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dummyModDir, "main.go"), []byte(mainContent), 0o644))

	origDir, getErr := os.Getwd()
	require.NoError(t, getErr)
	defer func() {
		_ = os.Chdir(origDir)
	}()
	require.NoError(t, os.Chdir(dummyModDir))

	ctx := t.Context()
	moduleDirs := map[string]bool{".": true}

	res, cleanup, err := AutoPin(ctx, moduleDirs, []string{"."})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, cleanup)
	cleanup()
}

func TestPin_Update(t *testing.T) {
	origVersion := util.Version
	util.Version = "v0.5.0"
	defer func() {
		util.Version = origVersion
	}()

	tmpDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmpDir)
	t.Setenv("GOWORK", "off")

	cwd, err := os.Getwd()
	require.NoError(t, err)
	repoRoot := filepath.Join(cwd, "..", "..", "..")

	// Create a dummy Go module
	dummyModDir := filepath.Join(tmpDir, "dummy")
	require.NoError(t, os.MkdirAll(dummyModDir, 0o755))

	// Write go.mod with replace directive pointing to local repo
	goModContent := fmt.Sprintf("module example.com/dummy\n\ngo 1.25\n\nreplace github.com/open-telemetry/opentelemetry-go-compile-instrumentation => %s\n", filepath.ToSlash(repoRoot))
	require.NoError(t, os.WriteFile(filepath.Join(dummyModDir, "go.mod"), []byte(goModContent), 0o644))

	// Write a simple main.go
	mainContent := `package main
import "fmt"
func main() {
	fmt.Println("hello")
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dummyModDir, "main.go"), []byte(mainContent), 0o644))

	// Write a dummy otel.instrumentation.go file
	toolFileContent := `package main

import (
	_ "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/cmd/otelc"
)
`
	require.NoError(t, os.WriteFile(filepath.Join(dummyModDir, ToolFileCanonical), []byte(toolFileContent), 0o644))

	origDir, getErr := os.Getwd()
	require.NoError(t, getErr)
	defer func() {
		_ = os.Chdir(origDir)
	}()
	require.NoError(t, os.Chdir(dummyModDir))

	ctx := t.Context()
	opts := PinOptions{
		Prune:    true,
		Validate: true,
		Args:     []string{"."},
	}

	// Call Pin. It should find the existing tool file and run updatePinnedProjects.
	res, err := Pin(ctx, opts)
	require.NoError(t, err)
	require.NotNil(t, res)
}





