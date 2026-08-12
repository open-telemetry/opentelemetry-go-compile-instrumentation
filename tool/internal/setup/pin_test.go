// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bytes"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
				"otelc" +
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
				"otelc" +
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
				"otelc" +
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
				"otelc" +
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

			actualNorm := strings.ReplaceAll(string(actual), "\r\n", "\n")
			golden.Assert(t, actualNorm, tt.goldenFile)
		})
	}
}

func TestLoadOtelYAMLImports(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
		want    map[string]bool
		wantErr string
	}{
		{
			name: "valid",
			content: `instrumentations:
  - example.com/foo
  - example.com/bar
  - example.com/foo
`,
			want: map[string]bool{
				"example.com/foo": true,
				"example.com/bar": true,
			},
		},
		{
			name: "missing instrumentations",
			content: `other:
  - example.com/foo
`,
			wantErr: "field other not found",
		},
		{
			name: "empty list",
			content: `instrumentations: []
`,
			want: map[string]bool{},
		},
		{
			name: "multiple documents",
			content: `instrumentations: []
---
instrumentations: []
`,
			wantErr: "multiple YAML documents",
		},
		{
			name: "empty import path",
			content: `instrumentations:
  - example.com/foo
  - "   "
`,
			wantErr: "instrumentations must not contain empty import paths",
		},
		{
			name: "malformed yaml",
			content: `instrumentations: [
`,
			wantErr: "parsing",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, InstrumentationYAMLCanonical)
			require.NoError(t, os.WriteFile(path, []byte(tt.content), 0o644))

			got, err := loadOtelYAMLImports(path)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLoadOtelYAMLImportsReadError(t *testing.T) {
	_, err := loadOtelYAMLImports(filepath.Join(t.TempDir(), "missing.yml"))
	require.ErrorContains(t, err, "reading")
}

func TestWriteOtelYAMLImports(t *testing.T) {
	path := filepath.Join(t.TempDir(), InstrumentationYAMLCanonical)
	require.NoError(t, os.WriteFile(path, []byte("instrumentations: []\n"), 0o600))

	require.NoError(t, writeOtelYAMLImports(path, map[string]bool{
		"example.com/z": true,
		"example.com/a": true,
	}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Less(t, strings.Index(string(data), "example.com/a"), strings.Index(string(data), "example.com/z"))
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(path)
		require.NoError(t, statErr)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	err = writeOtelYAMLImports(filepath.Join(t.TempDir(), "missing", InstrumentationYAMLCanonical), nil)
	require.ErrorContains(t, err, "stating")
}

func TestBackupAndRestoreFiles(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing")
	created := filepath.Join(dir, "created")
	require.NoError(t, os.WriteFile(existing, []byte("original"), 0o600))

	existingBackup, err := backupFile(existing)
	require.NoError(t, err)
	createdBackup, err := backupFile(created)
	require.NoError(t, err)
	require.True(t, existingBackup.existed)
	require.False(t, createdBackup.existed)

	require.NoError(t, os.WriteFile(existing, []byte("changed"), 0o644))
	require.NoError(t, os.WriteFile(created, []byte("temporary"), 0o644))
	require.NoError(t, restoreFiles([]fileBackup{existingBackup, createdBackup}))

	data, err := os.ReadFile(existing)
	require.NoError(t, err)
	require.Equal(t, []byte("original"), data)
	require.NoFileExists(t, created)
	require.NoError(t, restoreFiles([]fileBackup{createdBackup}))
}

func TestRestoreOtelYAMLValidationFilesJoinsErrors(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "keep"), []byte("data"), 0o644))
	result := &PinResult{}

	got, err := restoreOtelYAMLValidationFiles(
		result,
		errors.New("pin failed"),
		[]fileBackup{{path: dir}},
	)
	require.Same(t, result, got)
	require.ErrorContains(t, err, "pin failed")
	require.Error(t, err)
}

func TestLoadOtelYAMLImportsMalformedSecondDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), InstrumentationYAMLCanonical)
	require.NoError(t, os.WriteFile(path, []byte(`instrumentations: []
---
instrumentations: [
`), 0o644))

	_, err := loadOtelYAMLImports(path)
	require.ErrorContains(t, err, "parsing")
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
`, "go.opentelemetry.io/otelc", testVersion),
			wantModified: true,
			wantVersion:  testVersion,
		},
		{
			name: "keeps existing version",
			initial: fmt.Sprintf(`module example.com/test

go 1.25

tool %s

require %s %s
`, "go.opentelemetry.io/otelc/tool/cmd/otelc", "go.opentelemetry.io/otelc", testVersion),
			wantModified: false,
			wantVersion:  testVersion,
		},
		{
			name: "keeps newer version",
			initial: fmt.Sprintf(`module example.com/test

go 1.25

tool %s

require %s v1.99.0
`, "go.opentelemetry.io/otelc/tool/cmd/otelc", "go.opentelemetry.io/otelc"),
			wantModified: false,
			wantVersion:  "v1.99.0",
		},
		{
			name: "upgrades older version",
			initial: fmt.Sprintf(`module example.com/test

go 1.25

tool %s

require %s v1.0.0
`, "go.opentelemetry.io/otelc/tool/cmd/otelc", "go.opentelemetry.io/otelc"),
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
				if tool.Path != "go.opentelemetry.io/otelc/tool/cmd/otelc" {
					continue
				}

				foundTool = true
				break
			}

			var foundRequire bool
			for _, req := range f.Require {
				if req.Mod.Path != "go.opentelemetry.io/otelc" {
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
			name: "empty dependency version skips version-gated rule",
			deps: []*Dependency{
				{
					ImportPath: "example.com/foo",
					Version:    "", // replace/local path: findModVersion returns empty
				},
			},
			rules: map[string][]yamlRule{
				"example.com/instrumentation/foo": {{
					Target:       "example.com/foo",
					VersionRange: "v1.0.0",
				}},
			},
			want: map[string]bool{},
		},
		{
			name: "empty dependency version still matches when rule has no version range",
			deps: []*Dependency{
				{
					ImportPath: "example.com/foo",
					Version:    "",
				},
			},
			rules: map[string][]yamlRule{
				"example.com/instrumentation/foo": {{
					Target:       "example.com/foo",
					VersionRange: "",
				}},
			},
			want: map[string]bool{
				"example.com/instrumentation/foo": true,
			},
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
			name: "glob target mismatch",
			deps: []*Dependency{
				{
					ImportPath: "other.com/foo",
					Version:    "v1.2.3",
				},
			},
			rules: map[string][]yamlRule{
				"example.com/instrumentation/foo": {{
					Target:       "example.com/*",
					VersionRange: "v1.2.3",
				}},
			},
			want: map[string]bool{},
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
			got := matchInstrumentationImports(tt.deps, tt.rules, nil)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMatchInstrumentationImports_WarnsOnUnresolvedVersion(t *testing.T) {
	t.Run("warns when instrumentation is fully skipped", func(t *testing.T) {
		deps := []*Dependency{{
			ImportPath: "example.com/foo",
			Version:    "",
		}}
		rules := map[string][]yamlRule{
			"example.com/instrumentation/foo": {{
				Target:       "example.com/foo",
				VersionRange: "v1.0.0",
			}},
		}

		var warned bool
		var warnedMsg string
		got := matchInstrumentationImports(deps, rules, func(msg string, args ...any) {
			warned = true
			warnedMsg = msg
			_ = args
		})

		require.Empty(t, got)
		require.True(t, warned)
		require.Contains(t, warnedMsg, "unresolved")
	})

	t.Run("no warn when another rule still imports the module", func(t *testing.T) {
		// One dep fails version gate (empty version); another dep under the
		// same instrumentation module matches an unversioned rule. The module
		// must be imported and must not emit a skip warning.
		deps := []*Dependency{
			{ImportPath: "example.com/foo/v1", Version: ""},
			{ImportPath: "example.com/foo/v1/sub", Version: "v1.0.0"},
		}
		rules := map[string][]yamlRule{
			"example.com/instrumentation/foo": {
				{Target: "example.com/foo/v1", VersionRange: "v1.0.0"},
				{Target: "example.com/foo/v1/sub", VersionRange: ""},
			},
		}

		var warned bool
		got := matchInstrumentationImports(deps, rules, func(msg string, args ...any) {
			warned = true
			_ = msg
			_ = args
		})

		require.Equal(t, map[string]bool{"example.com/instrumentation/foo": true}, got)
		require.False(t, warned)
	})

	t.Run("warns once per instrumentation module", func(t *testing.T) {
		deps := []*Dependency{{
			ImportPath: "example.com/foo",
			Version:    "",
		}}
		rules := map[string][]yamlRule{
			"example.com/instrumentation/foo": {
				{Target: "example.com/foo", VersionRange: "v1.0.0"},
				{Target: "example.com/foo", VersionRange: "v2.0.0"},
			},
		}

		warnCount := 0
		got := matchInstrumentationImports(deps, rules, func(msg string, args ...any) {
			warnCount++
			_ = msg
			_ = args
		})

		require.Empty(t, got)
		require.Equal(t, 1, warnCount)
	})
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

	require.Contains(t, string(goMod), "go.opentelemetry.io/otelc")
	require.Contains(t, string(goMod), "go.opentelemetry.io/otelc/tool/cmd/otelc")
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

	err := updatePinnedProjects(t.Context(), []string{toolFile}, PinOptions{
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

	err := updatePinnedProjects(
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

	err := updatePinnedProjects(
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
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755)) // ensure .otelc-build exists

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte(`module example.com/test

go 1.25

require github.com/anthropics/anthropic-sdk-go v0.0.0-00010101000000-000000000000
replace github.com/anthropics/anthropic-sdk-go => ./anthropic
`),
		0o644,
	))

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "anthropic"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "anthropic", "go.mod"),
		[]byte(`module github.com/anthropics/anthropic-sdk-go

go 1.25
`),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "anthropic", "main.go"),
		[]byte(`package anthropic`),
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
	require.Contains(t, string(goMod), "go.opentelemetry.io/otelc/tool/cmd/otelc")
	require.Contains(t, string(goMod), "go.opentelemetry.io/otelc")
}

func TestProcessOtelYAMLFiles_AutoPin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, dir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		fmt.Appendf(nil, `module example.com/app

go 1.25

replace example.com/foo => %s
`, filepath.Join(dir, "foo")),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"),
		0o644,
	))
	writeInstrumentationModule(t, filepath.Join(dir, "foo"), "example.com/foo", true, nil)
	writeOtelYAMLFile(t, filepath.Join(dir, InstrumentationYAMLCanonical), "example.com/foo")

	originalYAML, err := os.ReadFile(filepath.Join(dir, InstrumentationYAMLCanonical))
	require.NoError(t, err)

	result, err := processOtelYAMLFiles(
		t.Context(),
		[]modulePinConfig{{moduleDir: dir, yamlFile: filepath.Join(dir, InstrumentationYAMLCanonical)}},
		PinOptions{AutoPin: true},
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.AllDeps)

	toolFile := filepath.Join(dir, ToolFileCanonical)
	require.FileExists(t, toolFile)

	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	require.Contains(t, string(data), `"example.com/foo"`)

	goMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	require.Contains(t, string(goMod), "go.opentelemetry.io/otelc/tool/cmd/otelc")
	require.Contains(t, string(goMod), "go.opentelemetry.io/otelc")
	require.Contains(t, string(goMod), "example.com/foo")

	afterYAML, err := os.ReadFile(filepath.Join(dir, InstrumentationYAMLCanonical))
	require.NoError(t, err)
	require.YAMLEq(t, string(originalYAML), string(afterYAML))
}

func TestPinLocked_MergesToolFileAndInstrumentationYAML(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		fmt.Appendf(nil, `module example.com/app

go 1.25

require example.com/foo v0.0.0-00010101000000-000000000000

replace example.com/foo => %s
replace example.com/bar => %s
`, filepath.Join(dir, "foo"), filepath.Join(dir, "bar")),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"),
		0o644,
	))

	writeInstrumentationModule(t, filepath.Join(dir, "foo"), "example.com/foo", true, nil)
	writeInstrumentationModule(t, filepath.Join(dir, "bar"), "example.com/bar", true, nil)
	toolFile := filepath.Join(dir, ToolFileCanonical)
	require.NoError(t, os.WriteFile(toolFile, []byte(`//go:build tools

package tools

import _ "example.com/foo"

// keep this comment
const Sentinel = "keep-me"

func Hello() string {
	return Sentinel
}
`), 0o644))
	writeOtelYAMLFile(t, filepath.Join(dir, InstrumentationYAMLCanonical), "example.com/bar")

	_, err := pinLocked(t.Context(), PinOptions{ModuleDirs: map[string]bool{dir: true}})
	require.NoError(t, err)

	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	contents := string(data)
	require.Equal(t, 1, strings.Count(contents, `"example.com/foo"`))
	require.NotContains(t, contents, `"example.com/bar"`)
	yamlImports, err := loadOtelYAMLImports(filepath.Join(dir, InstrumentationYAMLCanonical))
	require.NoError(t, err)
	require.True(t, yamlImports["example.com/bar"])
	require.Contains(t, contents, "// keep this comment")
	require.Contains(t, contents, `const Sentinel = "keep-me"`)
	require.Contains(t, contents, "func Hello() string")
	_, err = ast.NewAstParser().Parse(toolFile, parser.ParseComments)
	require.NoError(t, err)
}

func TestPinLocked_PrunePreservesToolAndYAMLOwnership(t *testing.T) {
	tmp := t.TempDir()
	app := filepath.Join(tmp, "app")
	toolInstrumentation := filepath.Join(tmp, "tool")
	yamlInstrumentation := filepath.Join(tmp, "yaml")

	writeInstrumentationModule(t, toolInstrumentation, "example.com/tool", true, nil)
	writeInstrumentationModule(t, yamlInstrumentation, "example.com/yaml", true, nil)
	writeInstrumentationModule(t, app, "example.com/app", false, map[string]string{
		"example.com/tool": toolInstrumentation,
		"example.com/yaml": yamlInstrumentation,
	})
	toolFile := filepath.Join(app, ToolFileCanonical)
	writeToolFile(t, toolFile, "example.com/tool")
	yamlFile := filepath.Join(app, InstrumentationYAMLCanonical)
	writeOtelYAMLFile(t, yamlFile, "example.com/yaml")
	originalTool, err := os.ReadFile(toolFile)
	require.NoError(t, err)

	_, err = pinLocked(t.Context(), PinOptions{
		ModuleDirs: map[string]bool{app: true},
		Prune:      true,
	})
	require.NoError(t, err)

	afterTool, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	require.Equal(t, originalTool, afterTool)
	yamlImports, err := loadOtelYAMLImports(yamlFile)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"example.com/yaml": true}, yamlImports)
}

func TestPinLocked_ExplicitConfigurationSuppressesWorkspaceInference(t *testing.T) {
	tmp := t.TempDir()
	configured := filepath.Join(tmp, "configured")
	unconfigured := filepath.Join(tmp, "unconfigured")
	instrumentation := filepath.Join(tmp, "instrumentation")

	writeInstrumentationModule(t, instrumentation, "example.com/instrumentation", true, nil)
	writeInstrumentationModule(t, configured, "example.com/configured", false, map[string]string{
		"example.com/instrumentation": instrumentation,
	})
	writeInstrumentationModule(t, unconfigured, "example.com/unconfigured", false, nil)

	_, err := pinLocked(t.Context(), PinOptions{ModuleDirs: map[string]bool{
		configured:   true,
		unconfigured: true,
	}})
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(unconfigured, ToolFileCanonical))
}

func TestDiscoverPinConfigs(t *testing.T) {
	tmp := t.TempDir()
	toolOnly := filepath.Join(tmp, "a-tool")
	yamlOnly := filepath.Join(tmp, "b-yaml")
	both := filepath.Join(tmp, "c-both")
	unconfigured := filepath.Join(tmp, "d-unconfigured")
	for _, dir := range []string{toolOnly, yamlOnly, both, unconfigured} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	writeToolFile(t, filepath.Join(toolOnly, ToolFileCanonical), "example.com/tool")
	writeOtelYAMLFile(t, filepath.Join(yamlOnly, InstrumentationYAMLCanonical), "example.com/yaml")
	writeToolFile(t, filepath.Join(both, ToolFileAlias), "example.com/tool")
	writeOtelYAMLFile(t, filepath.Join(both, InstrumentationYAMLAlias), "example.com/yaml")

	configs, unconfiguredDirs, err := discoverPinConfigs(map[string]bool{
		unconfigured: true,
		both:         true,
		yamlOnly:     true,
		toolOnly:     true,
	})
	require.NoError(t, err)
	require.Equal(t, []modulePinConfig{
		{moduleDir: toolOnly, toolFile: filepath.Join(toolOnly, ToolFileCanonical)},
		{moduleDir: yamlOnly, yamlFile: filepath.Join(yamlOnly, InstrumentationYAMLCanonical)},
		{
			moduleDir: both,
			toolFile:  filepath.Join(both, ToolFileAlias),
			yamlFile:  filepath.Join(both, InstrumentationYAMLAlias),
		},
	}, configs)
	require.Equal(t, map[string]bool{unconfigured: true}, unconfiguredDirs)
}

func TestDiscoverPinConfigsConflicts(t *testing.T) {
	t.Run("tool files", func(t *testing.T) {
		dir := t.TempDir()
		writeToolFile(t, filepath.Join(dir, ToolFileCanonical), "example.com/canonical")
		writeToolFile(t, filepath.Join(dir, ToolFileAlias), "example.com/alias")

		_, _, err := discoverPinConfigs(map[string]bool{dir: true})
		require.Error(t, err)
	})

	t.Run("yaml files", func(t *testing.T) {
		dir := t.TempDir()
		writeOtelYAMLFile(t, filepath.Join(dir, InstrumentationYAMLCanonical), "example.com/canonical")
		writeOtelYAMLFile(t, filepath.Join(dir, InstrumentationYAMLAlias), "example.com/alias")

		_, _, err := discoverPinConfigs(map[string]bool{dir: true})
		require.Error(t, err)
	})
}

func TestMaterializeModuleConfigsErrors(t *testing.T) {
	t.Run("malformed tool file", func(t *testing.T) {
		dir := t.TempDir()
		toolFile := filepath.Join(dir, ToolFileCanonical)
		require.NoError(t, os.WriteFile(toolFile, []byte("not go"), 0o644))

		_, err := materializeModuleConfigs(t.Context(), []modulePinConfig{{
			moduleDir: dir,
			toolFile:  toolFile,
		}}, PinOptions{})
		require.ErrorContains(t, err, "parsing tool file")
	})

	t.Run("malformed yaml", func(t *testing.T) {
		dir := t.TempDir()
		yamlFile := filepath.Join(dir, InstrumentationYAMLCanonical)
		require.NoError(t, os.WriteFile(yamlFile, []byte("instrumentations: ["), 0o644))

		_, err := materializeModuleConfigs(t.Context(), []modulePinConfig{{
			moduleDir: dir,
			yamlFile:  yamlFile,
		}}, PinOptions{})
		require.ErrorContains(t, err, "parsing")
	})
}

func TestProcessPinConfigsMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	yamlFile := filepath.Join(dir, InstrumentationYAMLCanonical)
	require.NoError(t, os.WriteFile(yamlFile, []byte("instrumentations: ["), 0o644))

	err := processPinConfigs(t.Context(), []modulePinConfig{{
		moduleDir: dir,
		yamlFile:  yamlFile,
	}}, PinOptions{})
	require.ErrorContains(t, err, "parsing")
}

func TestProcessPinConfigsAutoPinMaterializeError(t *testing.T) {
	dir := t.TempDir()
	toolFile := filepath.Join(dir, ToolFileCanonical)
	require.NoError(t, os.WriteFile(toolFile, []byte("not go"), 0o644))

	err := processPinConfigs(t.Context(), []modulePinConfig{{
		moduleDir: dir,
		toolFile:  toolFile,
	}}, PinOptions{AutoPin: true})
	require.ErrorContains(t, err, "parsing tool file")
}

func TestBackupOtelYAMLValidationFilesError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "go.mod"), 0o755))

	_, err := backupOtelYAMLValidationFiles([]modulePinConfig{{moduleDir: dir}})
	require.ErrorContains(t, err, "reading")
}

func TestApplyOtelYAMLValidationPruneWriteError(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "module")

	err := applyOtelYAMLValidation(
		map[string]string{moduleDir: filepath.Join(dir, "missing.yml")},
		map[string]map[string]bool{moduleDir: {}},
		nil,
		PinOptions{Prune: true},
	)
	require.ErrorContains(t, err, "stating")
}

func TestFindPinModuleDirsUsesProvidedModules(t *testing.T) {
	moduleDirs := map[string]bool{"example": true}
	opts := PinOptions{ModuleDirs: moduleDirs}

	got, err := findPinModuleDirs(t.Context(), &opts)
	require.NoError(t, err)
	require.Equal(t, moduleDirs, got)
}

func TestFindPinModuleDirsDiscoversModule(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, dir)
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/app\n\ngo 1.25\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"),
		0o644,
	))

	opts := PinOptions{Args: []string{"."}}
	moduleDirs, err := findPinModuleDirs(t.Context(), &opts)
	require.NoError(t, err)
	require.Len(t, moduleDirs, 1)
	for moduleDir := range moduleDirs {
		require.Equal(t, filepath.Clean(dir), filepath.Clean(moduleDir))
	}
}

func TestPinLockedFindModuleDirsError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, dir)
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/app\n\ngo 1.25\n"),
		0o644,
	))

	result, err := pinLocked(t.Context(), PinOptions{Args: []string{"./does-not-exist"}})
	require.Nil(t, result)
	require.ErrorContains(t, err, "getting build packages")
}

func TestPinLockedDiscoveryConflict(t *testing.T) {
	dir := t.TempDir()
	writeToolFile(t, filepath.Join(dir, ToolFileCanonical), "example.com/canonical")
	writeToolFile(t, filepath.Join(dir, ToolFileAlias), "example.com/alias")

	result, err := pinLocked(t.Context(), PinOptions{ModuleDirs: map[string]bool{dir: true}})
	require.Nil(t, result)
	require.Error(t, err)
}

func TestPinLockedEmptyModuleDirs(t *testing.T) {
	result, err := pinLocked(t.Context(), PinOptions{ModuleDirs: map[string]bool{"missing": true}})
	require.Error(t, err)
	require.Nil(t, result)
}

func TestPinLocked_RestoresAliasToolFileAfterYAMLValidation(t *testing.T) {
	tmp := t.TempDir()
	app := filepath.Join(tmp, "app")
	foo := filepath.Join(tmp, "foo")

	writeInstrumentationModule(t, foo, "example.com/foo", true, nil)
	writeInstrumentationModule(t, app, "example.com/app", false, map[string]string{
		"example.com/foo": foo,
	})
	canonical := filepath.Join(app, ToolFileCanonical)
	alias := filepath.Join(app, ToolFileAlias)
	require.NoError(t, os.Rename(canonical, alias))
	writeOtelYAMLFile(t, filepath.Join(app, InstrumentationYAMLCanonical), "example.com/foo")

	original, err := os.ReadFile(alias)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(alias, 0o600))

	_, err = pinLocked(t.Context(), PinOptions{ModuleDirs: map[string]bool{app: true}})
	require.NoError(t, err)

	after, err := os.ReadFile(alias)
	require.NoError(t, err)
	require.Equal(t, original, after)
	info, err := os.Stat(alias)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	require.NoFileExists(t, canonical)
}

func TestPinLocked_RestoresAliasToolFileOnYAMLValidationFailure(t *testing.T) {
	tmp := t.TempDir()
	app := filepath.Join(tmp, "app")
	missing := filepath.Join(tmp, "missing")

	writeInstrumentationModule(t, app, "example.com/app", false, map[string]string{
		"example.com/missing": missing,
	})
	canonical := filepath.Join(app, ToolFileCanonical)
	alias := filepath.Join(app, ToolFileAlias)
	require.NoError(t, os.Rename(canonical, alias))
	writeOtelYAMLFile(t, filepath.Join(app, InstrumentationYAMLCanonical), "example.com/missing")

	originalTool, err := os.ReadFile(alias)
	require.NoError(t, err)
	originalMod, err := os.ReadFile(filepath.Join(app, "go.mod"))
	require.NoError(t, err)

	_, err = pinLocked(t.Context(), PinOptions{ModuleDirs: map[string]bool{app: true}})
	require.Error(t, err)

	afterTool, readErr := os.ReadFile(alias)
	require.NoError(t, readErr)
	require.Equal(t, originalTool, afterTool)
	afterMod, readErr := os.ReadFile(filepath.Join(app, "go.mod"))
	require.NoError(t, readErr)
	require.Equal(t, originalMod, afterMod)
	require.NoFileExists(t, canonical)
}

func TestAddToolFileImports_PreservesDeclarationsWithoutImportBlock(t *testing.T) {
	toolFile := filepath.Join(t.TempDir(), ToolFileCanonical)
	require.NoError(t, os.WriteFile(toolFile, []byte(`//go:build tools

package tools

// keep this comment
const Sentinel = "keep-me"
`), 0o644))

	require.NoError(t, addToolFileImports(toolFile, map[string]bool{
		"example.com/bar": true,
		"example.com/foo": true,
	}))

	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	contents := string(data)
	require.Equal(t, 1, strings.Count(contents, `_ "example.com/bar"`))
	require.Equal(t, 1, strings.Count(contents, `_ "example.com/foo"`))
	require.Contains(t, contents, "// keep this comment")
	require.Contains(t, contents, `const Sentinel = "keep-me"`)
	_, err = ast.NewAstParser().Parse(toolFile, parser.ParseComments)
	require.NoError(t, err)
}

func TestAddToolFileImports_DeduplicatesExistingBlock(t *testing.T) {
	toolFile := filepath.Join(t.TempDir(), ToolFileCanonical)
	writeToolFile(t, toolFile, "example.com/foo")

	require.NoError(t, addToolFileImports(toolFile, map[string]bool{
		"example.com/foo": true,
		"example.com/bar": true,
	}))

	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(data), `"example.com/foo"`))
	require.Equal(t, 1, strings.Count(string(data), `"example.com/bar"`))
}

func TestInvalidInstrumentationImports_ValidatesRules(t *testing.T) {
	tmp := t.TempDir()
	app := filepath.Join(tmp, "app")
	instrumentation := filepath.Join(tmp, "instrumentation")
	toolFile := writeInstrumentationModule(t, app, "example.com/app", false, map[string]string{
		"example.com/instrumentation": instrumentation,
	})
	writeInstrumentationModule(t, instrumentation, "example.com/instrumentation", false, nil)
	require.NoError(t, os.WriteFile(
		filepath.Join(instrumentation, "invalid.otelc.yml"),
		[]byte("invalid: yaml: {"),
		0o644,
	))

	invalid, err := invalidInstrumentationImports(t.Context(), []string{toolFile}, true)
	require.NoError(t, err)
	require.Equal(t, map[string]map[string]bool{
		toolFile: {"example.com/instrumentation": true},
	}, invalid)

	invalid, err = invalidInstrumentationImports(t.Context(), []string{toolFile}, false)
	require.NoError(t, err)
	require.Empty(t, invalid)
}

func TestPinLocked_MixedWorkspaceSources(t *testing.T) {
	tmp := t.TempDir()
	toolModule := filepath.Join(tmp, "tool-module")
	yamlModule := filepath.Join(tmp, "yaml-module")
	foo := filepath.Join(tmp, "foo")
	bar := filepath.Join(tmp, "bar")

	writeInstrumentationModule(t, foo, "example.com/foo", true, nil)
	writeInstrumentationModule(t, bar, "example.com/bar", true, nil)
	writeInstrumentationModule(t, toolModule, "example.com/tool-app", false, map[string]string{
		"example.com/foo": foo,
	})
	writeInstrumentationModule(t, yamlModule, "example.com/yaml-app", false, map[string]string{
		"example.com/bar": bar,
	})
	require.NoError(t, os.Remove(filepath.Join(yamlModule, ToolFileCanonical)))
	writeOtelYAMLFile(t, filepath.Join(yamlModule, InstrumentationYAMLCanonical), "example.com/bar")

	_, err := pinLocked(t.Context(), PinOptions{
		ModuleDirs: map[string]bool{toolModule: true, yamlModule: true},
		AutoPin:    true,
		Prune:      true,
	})
	require.NoError(t, err)

	toolData, err := os.ReadFile(filepath.Join(toolModule, ToolFileCanonical))
	require.NoError(t, err)
	require.Contains(t, string(toolData), `"example.com/foo"`)

	yamlToolData, err := os.ReadFile(filepath.Join(yamlModule, ToolFileCanonical))
	require.NoError(t, err)
	require.Contains(t, string(yamlToolData), `"example.com/bar"`)
}

func TestPinLocked_YAMLPruning(t *testing.T) {
	for _, prune := range []bool{false, true} {
		t.Run(fmt.Sprintf("prune_%t", prune), func(t *testing.T) {
			tmp := t.TempDir()
			app := filepath.Join(tmp, "app")
			valid := filepath.Join(tmp, "valid")
			invalid := filepath.Join(tmp, "invalid")

			writeInstrumentationModule(t, valid, "example.com/valid", true, nil)
			writeInstrumentationModule(t, invalid, "example.com/invalid", false, nil)
			writeInstrumentationModule(t, app, "example.com/app", false, map[string]string{
				"example.com/valid":   valid,
				"example.com/invalid": invalid,
			})
			require.NoError(t, os.Remove(filepath.Join(app, ToolFileCanonical)))
			yamlPath := filepath.Join(app, InstrumentationYAMLCanonical)
			writeOtelYAMLFile(t, yamlPath, "example.com/valid", "example.com/invalid")
			originalGoMod, err := os.ReadFile(filepath.Join(app, "go.mod"))
			require.NoError(t, err)

			_, err = pinLocked(t.Context(), PinOptions{
				ModuleDirs: map[string]bool{app: true},
				Prune:      prune,
			})
			require.NoError(t, err)
			_, statErr := os.Stat(filepath.Join(app, ToolFileCanonical))
			require.ErrorIs(t, statErr, os.ErrNotExist)

			data, err := os.ReadFile(yamlPath)
			require.NoError(t, err)
			require.Contains(t, string(data), "example.com/valid")
			if prune {
				require.NotContains(t, string(data), "example.com/invalid")
			} else {
				require.Contains(t, string(data), "example.com/invalid")
			}
			afterGoMod, err := os.ReadFile(filepath.Join(app, "go.mod"))
			require.NoError(t, err)
			require.Equal(t, originalGoMod, afterGoMod)
		})
	}
}

func TestPinLocked_YAMLPrunesToEmptyList(t *testing.T) {
	tmp := t.TempDir()
	app := filepath.Join(tmp, "app")
	invalid := filepath.Join(tmp, "invalid")
	writeInstrumentationModule(t, invalid, "example.com/invalid", false, nil)
	writeInstrumentationModule(t, app, "example.com/app", false, map[string]string{
		"example.com/invalid": invalid,
	})
	require.NoError(t, os.Remove(filepath.Join(app, ToolFileCanonical)))
	yamlPath := filepath.Join(app, InstrumentationYAMLCanonical)
	writeOtelYAMLFile(t, yamlPath, "example.com/invalid")

	_, err := pinLocked(t.Context(), PinOptions{ModuleDirs: map[string]bool{app: true}, Prune: true})
	require.NoError(t, err)
	imports, err := loadOtelYAMLImports(yamlPath)
	require.NoError(t, err)
	require.Empty(t, imports)
}

func TestAutoPin_RestoresYAMLWorkspace(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	app := filepath.Join(tmp, "app")
	instrumentation := filepath.Join(tmp, "instrumentation")

	writeInstrumentationModule(t, instrumentation, "example.com/instrumentation", true, nil)
	writeInstrumentationModule(t, app, "example.com/app", false, map[string]string{
		"example.com/instrumentation": instrumentation,
	})
	require.NoError(t, os.Remove(filepath.Join(app, ToolFileCanonical)))
	yamlPath := filepath.Join(app, InstrumentationYAMLCanonical)
	writeOtelYAMLFile(t, yamlPath, "example.com/instrumentation")
	originalYAML, err := os.ReadFile(yamlPath)
	require.NoError(t, err)

	stateManager := NewStateManager()
	ctx := ContextWithStateManager(t.Context(), stateManager)
	_, err = AutoPin(ctx, map[string]bool{app: true}, subcmdBuild, []string{"."})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(app, ToolFileCanonical))

	require.NoError(t, stateManager.Revert())
	_, statErr := os.Stat(filepath.Join(app, ToolFileCanonical))
	require.ErrorIs(t, statErr, os.ErrNotExist)
	afterYAML, err := os.ReadFile(yamlPath)
	require.NoError(t, err)
	require.True(t, bytes.Equal(originalYAML, afterYAML))
}

func TestAutoPinRequiresStateManager(t *testing.T) {
	result, err := AutoPin(t.Context(), map[string]bool{"module": true}, subcmdBuild, nil)
	require.Nil(t, result)
	require.ErrorContains(t, err, "state manager not found")
}
