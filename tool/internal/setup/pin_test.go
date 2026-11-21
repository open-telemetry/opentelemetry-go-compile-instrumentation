// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bytes"
	"context"
	"fmt"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
	"gotest.tools/v3/golden"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

const (
	unzippedInstDir = "instrumentation"
)

// discardLogger returns a logger that drops all output, keeping test logs quiet.
func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

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
			outPath := filepath.Join(tmpDir, toolFileCanonical)

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
	path := filepath.Join(t.TempDir(), instrumentationYAMLCanonical)
	require.NoError(t, os.WriteFile(path, []byte(`instrumentations:
  - example.com/foo
  - example.com/bar
  - example.com/foo
`), 0o644))
	imports, err := loadOtelYAMLImports(path)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"example.com/foo": true, "example.com/bar": true}, imports)

	require.NoError(t, os.WriteFile(path, []byte("unknown: true\n"), 0o644))
	_, err = loadOtelYAMLImports(path)
	require.ErrorContains(t, err, "field unknown not found")

	for _, contents := range []string{
		"", "# empty\n", "{}\n", "instrumentations: null\n", "instrumentations: []\n",
		"instrumentations: []\n---\n", "instrumentations: []\n---\n# trailing comment\n",
	} {
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
		imports, err = loadOtelYAMLImports(path)
		require.NoError(t, err)
		require.Empty(t, imports)
	}

	for _, tt := range []struct {
		name     string
		contents string
		want     string
	}{
		{name: "malformed", contents: "instrumentations: [", want: "parsing"},
		{name: "multiple documents", contents: "instrumentations: []\n---\ninstrumentations: []\n", want: "multiple YAML documents"},
		{name: "malformed second document", contents: "instrumentations: []\n---\n[", want: "parsing"},
		{
			name:     "populated document after empty document",
			contents: "instrumentations: []\n---\n# empty\n---\ninstrumentations: [example.com/third]\n",
			want:     "multiple YAML documents",
		},
		{name: "empty import", contents: "instrumentations:\n  - '  '\n", want: "must not contain empty import paths"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(path, []byte(tt.contents), 0o644))
			_, loadErr := loadOtelYAMLImports(path)
			require.ErrorContains(t, loadErr, tt.want)
		})
	}

	_, err = loadOtelYAMLImports(filepath.Join(t.TempDir(), "missing.yml"))
	require.ErrorContains(t, err, "reading")
}

func TestWriteOtelYAMLImports(t *testing.T) {
	path := filepath.Join(t.TempDir(), instrumentationYAMLCanonical)
	original := []byte(`# hand-maintained config
instrumentations:
  - 'example.com/z'
  # retain this comment
  - example.com/a # retain inline comment
`)
	require.NoError(t, os.WriteFile(path, original, 0o640))

	// A no-op prune must preserve the file byte-for-byte.
	require.NoError(t, writeOtelYAMLImports(path, map[string]bool{
		"example.com/z": true,
		"example.com/a": true,
	}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, data)

	// Real pruning retains the original order and comments associated with the
	// surviving scalar while dropping only the invalid selection.
	require.NoError(t, writeOtelYAMLImports(path, map[string]bool{"example.com/a": true}))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "# hand-maintained config")
	require.Contains(t, string(data), "# retain this comment")
	require.Contains(t, string(data), "# retain inline comment")
	require.Contains(t, string(data), "example.com/a")
	require.NotContains(t, string(data), "example.com/z")
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(path)
		require.NoError(t, statErr)
		require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	}

	err = writeOtelYAMLImports(filepath.Join(t.TempDir(), "missing", "config.yml"), nil)
	require.ErrorContains(t, err, "reading")
}

func TestYAMLStateManager(t *testing.T) {
	manager, err := yamlStateManager(t.Context(), false)
	require.NoError(t, err)
	require.NotNil(t, manager)

	_, err = yamlStateManager(t.Context(), true)
	require.ErrorContains(t, err, "state manager not found")

	want := newStateManager()
	manager, err = yamlStateManager(contextWithStateManager(t.Context(), want), true)
	require.NoError(t, err)
	require.Same(t, want, manager)
}

func TestLoadYAMLStatesError(t *testing.T) {
	_, err := loadYAMLStates([]modulePinConfig{{yamlFile: filepath.Join(t.TempDir(), "missing.yml")}})
	require.Error(t, err)
}

func TestTrackYAMLConfigFilesRevertsExistingAndGeneratedFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	t.Setenv("GOWORK", "off")
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	moduleDir := filepath.Join(tmp, "module")
	require.NoError(t, os.MkdirAll(moduleDir, 0o755))
	goMod := filepath.Join(moduleDir, goModFileName)
	original := []byte("module example.com/test\n\ngo 1.25\n")
	require.NoError(t, os.WriteFile(goMod, original, 0o644))

	manager := newStateManager()
	require.NoError(t, trackYAMLConfigFiles(
		t.Context(), manager, []modulePinConfig{{moduleDir: moduleDir}}, PinOptions{},
	))
	for _, name := range []string{goModFileName, goSumFileName, toolFileCanonical, toolFileAlias} {
		path, err := filepath.Abs(filepath.Join(moduleDir, name))
		require.NoError(t, err)
		require.Contains(t, manager.files, filepath.Clean(path))
	}

	require.NoError(t, os.WriteFile(goMod, []byte("changed"), 0o644))
	generated := filepath.Join(moduleDir, toolFileCanonical)
	require.NoError(t, os.WriteFile(generated, []byte("generated"), 0o644))
	require.NoError(t, manager.Revert())

	got, err := os.ReadFile(goMod)
	require.NoError(t, err)
	require.Equal(t, original, got)
	require.NoFileExists(t, generated)
}

func TestTrackYAMLConfigFilesTracksWorkspaceSum(t *testing.T) {
	root := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, root)
	t.Setenv("GOWORK", "")
	moduleDir := filepath.Join(root, "module")
	require.NoError(t, os.MkdirAll(moduleDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(moduleDir, goModFileName), []byte("module example.com/test\n\ngo 1.25\n"), 0o644,
	))
	workFile := filepath.Join(root, "go.work")
	workSum := filepath.Join(root, "go.work.sum")
	require.NoError(t, os.WriteFile(workFile, []byte("go 1.26.0\n\nuse ./module\n"), 0o644))
	require.NoError(t, os.WriteFile(workSum, []byte("workspace sum\n"), 0o644))

	manager := newStateManager()
	require.NoError(t, trackYAMLConfigFiles(
		t.Context(), manager, []modulePinConfig{{moduleDir: moduleDir}},
		PinOptions{Args: []string{"-C", moduleDir}},
	))
	absWorkSum, err := filepath.Abs(workSum)
	require.NoError(t, err)
	require.Contains(t, manager.files, filepath.Clean(absWorkSum))
}

func TestProcessYAMLConfigsRequiresStateManagerForAutoPin(t *testing.T) {
	path := filepath.Join(t.TempDir(), instrumentationYAMLCanonical)
	require.NoError(t, os.WriteFile(path, []byte("instrumentations: []\n"), 0o644))
	err := processYAMLConfigs(t.Context(), []modulePinConfig{{yamlFile: path}}, PinOptions{AutoPin: true})
	require.ErrorContains(t, err, "state manager not found")
}

func TestProcessYAMLConfigsLoadError(t *testing.T) {
	err := processYAMLConfigs(t.Context(), []modulePinConfig{{
		yamlFile: filepath.Join(t.TempDir(), "missing.yml"),
	}}, PinOptions{})
	require.ErrorContains(t, err, "reading")
}

func TestFinalizeAutoPinYAMLTidyErrorRevertsToolFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	moduleDir := filepath.Join(tmp, "module")
	require.NoError(t, os.MkdirAll(moduleDir, 0o755))
	toolFile := filepath.Join(moduleDir, toolFileCanonical)
	manager := newStateManager()
	require.NoError(t, manager.Track(toolFile))

	err := finalizeAutoPinYAML(t.Context(), []yamlPinState{{
		config:   modulePinConfig{moduleDir: moduleDir},
		imports:  map[string]bool{"example.com/instrumentation": true},
		toolFile: toolFile,
	}}, PinOptions{})
	require.ErrorContains(t, err, "running go mod tidy")
	require.NoError(t, manager.Revert())
	require.NoFileExists(t, toolFile)
}

func TestFinalizeAutoPinYAMLWritesPrunedToolFile(t *testing.T) {
	tmp := t.TempDir()
	moduleDir := filepath.Join(tmp, "module")
	require.NoError(t, os.MkdirAll(moduleDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(moduleDir, goModFileName), []byte("module example.com/test\n\ngo 1.25\n"), 0o644,
	))
	toolFile := filepath.Join(moduleDir, toolFileCanonical)

	err := finalizeAutoPinYAML(t.Context(), []yamlPinState{{
		config:   modulePinConfig{moduleDir: moduleDir},
		imports:  map[string]bool{},
		toolFile: toolFile,
	}}, PinOptions{})
	require.NoError(t, err)

	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "package tools")
	require.NotContains(t, string(data), "import (")
}

func TestFinalizeStandaloneYAMLWriteError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	manager := newStateManager()
	generated := filepath.Join(tmp, toolFileCanonical)
	require.NoError(t, manager.Track(generated))
	require.NoError(t, os.WriteFile(generated, []byte("generated"), 0o644))

	err := finalizeStandaloneYAML([]yamlPinState{{
		config: modulePinConfig{yamlFile: filepath.Join(tmp, "missing.yml")},
	}}, PinOptions{Prune: true}, manager)
	require.ErrorContains(t, err, "reading")
	require.NoFileExists(t, generated)
}

func TestMaterializeYAMLConfigsInvalidGoMod(t *testing.T) {
	dir := t.TempDir()
	yamlFile := filepath.Join(dir, instrumentationYAMLCanonical)
	require.NoError(t, os.WriteFile(yamlFile, []byte("instrumentations: []\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, goModFileName), []byte("invalid"), 0o644))

	_, err := materializeYAMLConfigs(t.Context(), []modulePinConfig{{moduleDir: dir, yamlFile: yamlFile}}, PinOptions{})
	require.ErrorContains(t, err, "ensuring otelc require")
}

func TestProcessYAMLConfigsMaterializeErrorRevertsToolFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	yamlFile := filepath.Join(tmp, instrumentationYAMLCanonical)
	require.NoError(t, os.WriteFile(yamlFile, []byte("instrumentations: []\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, goModFileName), []byte("invalid"), 0o644))

	err := processYAMLConfigs(t.Context(), []modulePinConfig{{
		moduleDir: tmp,
		yamlFile:  yamlFile,
	}}, PinOptions{})
	require.ErrorContains(t, err, "ensuring otelc require")
	require.NoFileExists(t, filepath.Join(tmp, toolFileCanonical))
	require.NoFileExists(
		t,
		util.GetBuildTemp(stateFileName),
		"a reverted standalone pin must discard its recovery manifest",
	)
	goMod, readErr := os.ReadFile(filepath.Join(tmp, goModFileName))
	require.NoError(t, readErr)
	require.Equal(t, "invalid", string(goMod))
}

func TestProcessConfiguredModulesError(t *testing.T) {
	dir := t.TempDir()
	toolFile := filepath.Join(dir, toolFileCanonical)
	require.NoError(t, os.WriteFile(toolFile, []byte("not go"), 0o644))

	err := processConfiguredModules(t.Context(), []modulePinConfig{{moduleDir: dir, toolFile: toolFile}}, PinOptions{})
	require.Error(t, err)
}

func TestDiscoverPinConfigsKeepsUnconfiguredModules(t *testing.T) {
	configured := t.TempDir()
	unconfigured := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(configured, instrumentationYAMLCanonical),
		[]byte("instrumentations: []\n"), 0o644,
	))

	configs, inferred, err := discoverPinConfigs([]string{configured, unconfigured})
	require.NoError(t, err)
	require.Equal(t, []modulePinConfig{{
		moduleDir: configured,
		yamlFile:  filepath.Join(configured, instrumentationYAMLCanonical),
	}}, configs)
	require.Equal(t, map[string]bool{unconfigured: true}, inferred)
}

func TestDiscoverPinConfigsToolFileTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	toolFile := filepath.Join(dir, toolFileCanonical)
	require.NoError(t, os.WriteFile(toolFile, []byte("//go:build tools\n\npackage tools\n"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, instrumentationYAMLCanonical),
		[]byte("instrumentations:\n  - example.com/ignored\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, instrumentationYAMLAlias),
		[]byte("instrumentations:\n  - example.com/also-ignored\n"),
		0o644,
	))

	configs, unconfigured, err := discoverPinConfigs([]string{dir})
	require.NoError(t, err)
	require.Empty(t, unconfigured)
	require.Equal(t, []modulePinConfig{{moduleDir: dir, toolFile: toolFile}}, configs)
}

func TestDiscoverPinConfigsRejectsAmbiguousFiles(t *testing.T) {
	for _, tt := range []struct {
		name  string
		files []string
		want  string
	}{
		{
			name:  "tool files",
			files: []string{toolFileCanonical, toolFileAlias},
			want:  "only one instrumentation config file is allowed",
		},
		{
			name:  "YAML files",
			files: []string{instrumentationYAMLCanonical, instrumentationYAMLAlias},
			want:  "only one instrumentation YAML file is allowed",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tt.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o644))
			}

			configs, unconfigured, err := discoverPinConfigs([]string{dir})
			require.ErrorContains(t, err, tt.want)
			require.Nil(t, configs)
			require.Nil(t, unconfigured)
		})
	}
}

func TestPinLockedYAMLOnlyPrunesAndRestoresModuleState(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	root := filepath.Join(tmp, "root")
	invalid := filepath.Join(tmp, "invalid")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.MkdirAll(invalid, 0o755))
	goMod := fmt.Sprintf(`module example.com/root

go 1.25

require example.com/invalid v0.0.0

replace example.com/invalid => %s
`, invalid)
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(invalid, "go.mod"), []byte("module example.com/invalid\n\ngo 1.25\n"), 0o644,
	))
	require.NoError(t, os.WriteFile(filepath.Join(invalid, "invalid.go"), []byte("package invalid\n"), 0o644))
	yamlFile := filepath.Join(root, instrumentationYAMLCanonical)
	require.NoError(t, os.WriteFile(
		yamlFile, []byte("instrumentations:\n  - example.com/invalid\n"), 0o644,
	))

	beforeMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	_, err = pinLocked(t.Context(), PinOptions{Prune: true, ModuleDirs: []string{root}})
	require.NoError(t, err)

	afterMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.Equal(t, beforeMod, afterMod)
	require.NoFileExists(t, filepath.Join(root, toolFileCanonical))
	imports, err := loadOtelYAMLImports(yamlFile)
	require.NoError(t, err)
	require.Empty(t, imports)
}

func TestPinLockedYAMLOnlyWithoutPruneRestoresModuleState(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	root := filepath.Join(tmp, "root")
	invalid := filepath.Join(tmp, "invalid")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.MkdirAll(invalid, 0o755))
	goMod := fmt.Sprintf(`module example.com/root

go 1.25

require example.com/invalid v0.0.0

replace example.com/invalid => %s
`, invalid)
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(invalid, "go.mod"), []byte("module example.com/invalid\n\ngo 1.25\n"), 0o644,
	))
	require.NoError(t, os.WriteFile(filepath.Join(invalid, "invalid.go"), []byte("package invalid\n"), 0o644))
	yamlFile := filepath.Join(root, instrumentationYAMLCanonical)
	yamlData := []byte("# keep formatting\ninstrumentations:\n  - example.com/invalid\n")
	require.NoError(t, os.WriteFile(yamlFile, yamlData, 0o644))

	beforeMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	_, err = pinLocked(t.Context(), PinOptions{ModuleDirs: []string{root}})
	require.NoError(t, err)

	afterMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.Equal(t, beforeMod, afterMod)
	require.NoFileExists(t, filepath.Join(root, toolFileCanonical))
	afterYAML, err := os.ReadFile(yamlFile)
	require.NoError(t, err)
	require.True(t, bytes.Equal(yamlData, afterYAML))
}

func TestPinLockedYAMLValidationPrunesInvalidRule(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	root := filepath.Join(tmp, "root")
	valid := filepath.Join(tmp, "valid")
	invalid := filepath.Join(tmp, "invalid")
	require.NoError(t, os.MkdirAll(root, 0o755))
	writeInstrumentationModule(t, valid, "example.com/valid", true, nil)
	writeInstrumentationModule(t, invalid, "example.com/invalid", false, nil)
	require.NoError(t, os.WriteFile(
		filepath.Join(invalid, "invalid.otelc.yml"), []byte("invalid: yaml: {"), 0o644,
	))
	goMod := fmt.Sprintf(`module example.com/root

go 1.25

require (
	example.com/invalid v0.0.0
	example.com/valid v0.0.0
)

replace example.com/invalid => %s
replace example.com/valid => %s
`, invalid, valid)
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o644))
	yamlFile := filepath.Join(root, instrumentationYAMLCanonical)
	require.NoError(t, os.WriteFile(yamlFile, []byte(
		"instrumentations:\n  - example.com/invalid\n  - example.com/valid\n",
	), 0o644))

	beforeMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	_, err = pinLocked(t.Context(), PinOptions{
		Prune: true, Validate: true, ModuleDirs: []string{root},
	})
	require.NoError(t, err)

	afterMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.Equal(t, beforeMod, afterMod)
	require.NoFileExists(t, filepath.Join(root, toolFileCanonical))
	imports, err := loadOtelYAMLImports(yamlFile)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"example.com/valid": true}, imports)
}

func TestPinLockedProcessesToolAndYAMLModules(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	toolRoot := filepath.Join(tmp, "tool-root")
	toolInvalid := filepath.Join(tmp, "tool-invalid")
	toolFile := writeInstrumentationModule(t, toolRoot, "example.com/tool-root", false, map[string]string{
		"example.com/tool-invalid": toolInvalid,
	})
	writeInstrumentationModule(t, toolInvalid, "example.com/tool-invalid", false, nil)

	yamlRoot := filepath.Join(tmp, "yaml-root")
	yamlInvalid := filepath.Join(tmp, "yaml-invalid")
	require.NoError(t, os.MkdirAll(yamlRoot, 0o755))
	writeInstrumentationModule(t, yamlInvalid, "example.com/yaml-invalid", false, nil)
	yamlGoMod := fmt.Sprintf(`module example.com/yaml-root

go 1.25

require example.com/yaml-invalid v0.0.0

replace example.com/yaml-invalid => %s
`, yamlInvalid)
	require.NoError(t, os.WriteFile(filepath.Join(yamlRoot, "go.mod"), []byte(yamlGoMod), 0o644))
	yamlFile := filepath.Join(yamlRoot, instrumentationYAMLCanonical)
	require.NoError(t, os.WriteFile(
		yamlFile, []byte("instrumentations:\n  - example.com/yaml-invalid\n"), 0o644,
	))

	_, err := pinLocked(t.Context(), PinOptions{Prune: true, ModuleDirs: []string{
		toolRoot,
		yamlRoot,
	}})
	require.NoError(t, err)

	toolData, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	require.NotContains(t, string(toolData), "example.com/tool-invalid")
	yamlImports, err := loadOtelYAMLImports(yamlFile)
	require.NoError(t, err)
	require.Empty(t, yamlImports)
	require.NoFileExists(t, filepath.Join(yamlRoot, toolFileCanonical))
}

func TestPinLockedPrunesDuplicateInvalidYAMLSelectionsPerModule(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	t.Setenv("GOWORK", "off")
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	sourceRoot, err := repositorySourceRoot()
	require.NoError(t, err)
	t.Setenv(envOtelcSourceRoot, sourceRoot)

	invalid := filepath.Join(tmp, "invalid")
	writeInstrumentationModule(t, invalid, "example.com/invalid", false, nil)
	modules := []string{filepath.Join(tmp, "a"), filepath.Join(tmp, "b")}
	for _, moduleDir := range modules {
		name := filepath.Base(moduleDir)
		goMod := fmt.Sprintf(`module example.com/%s

go 1.25

require example.com/invalid v0.0.0

replace example.com/invalid => %s
`, name, invalid)
		require.NoError(t, os.MkdirAll(moduleDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(moduleDir, goModFileName), []byte(goMod), 0o644))
		require.NoError(t, os.WriteFile(
			filepath.Join(moduleDir, instrumentationYAMLCanonical),
			[]byte("# per-module selection\ninstrumentations:\n  - example.com/invalid\n"),
			0o644,
		))
	}

	_, err = pinLocked(t.Context(), PinOptions{Prune: true, ModuleDirs: modules})
	require.NoError(t, err)
	for _, moduleDir := range modules {
		imports, readErr := loadOtelYAMLImports(filepath.Join(moduleDir, instrumentationYAMLCanonical))
		require.NoError(t, readErr)
		require.Empty(t, imports, "each YAML root must prune its own invalid selection")
	}
}

func TestAutoPinFailureRestoresEveryModule(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	t.Setenv("GOWORK", "off")
	sourceRoot, err := repositorySourceRoot()
	require.NoError(t, err)
	t.Setenv(envOtelcSourceRoot, sourceRoot)

	valid := filepath.Join(tmp, "valid")
	writeInstrumentationModule(t, valid, "example.com/invalid", false, nil)
	first := filepath.Join(tmp, "a")
	second := filepath.Join(tmp, "b")
	firstMod := []byte(fmt.Sprintf(`module example.com/a

go 1.25

require example.com/invalid v0.0.0

replace example.com/invalid => %s
`, valid))
	secondMod := []byte("invalid go.mod")
	for _, moduleDir := range []string{first, second} {
		require.NoError(t, os.MkdirAll(moduleDir, 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(moduleDir, instrumentationYAMLCanonical),
			[]byte("instrumentations:\n  - example.com/invalid\n"),
			0o644,
		))
	}
	require.NoError(t, os.WriteFile(filepath.Join(first, goModFileName), firstMod, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(second, goModFileName), secondMod, 0o644))

	manager := newStateManager()
	_, err = autoPin(contextWithStateManager(t.Context(), manager), []string{first, second}, subcmdBuild, nil)
	require.ErrorContains(t, err, "ensuring otelc require")
	require.NoError(t, manager.Revert())
	require.NoError(t, manager.Discard())

	for moduleDir, wantMod := range map[string][]byte{first: firstMod, second: secondMod} {
		gotMod, readErr := os.ReadFile(filepath.Join(moduleDir, goModFileName))
		require.NoError(t, readErr)
		require.Equal(t, wantMod, gotMod)
		require.NoFileExists(t, filepath.Join(moduleDir, toolFileCanonical))
		require.NoFileExists(t, filepath.Join(moduleDir, goSumFileName))
	}
}

func TestPinLockedEmptyYAMLLeavesNoTemporaryState(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(tmp, "go.mod"), []byte("module example.com/root\n\ngo 1.25\n"), 0o644,
	))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, instrumentationYAMLCanonical), nil, 0o644))

	beforeMod, err := os.ReadFile(filepath.Join(tmp, "go.mod"))
	require.NoError(t, err)
	_, err = pinLocked(t.Context(), PinOptions{Prune: true, ModuleDirs: []string{tmp}})
	require.NoError(t, err)
	afterMod, err := os.ReadFile(filepath.Join(tmp, "go.mod"))
	require.NoError(t, err)
	require.Equal(t, beforeMod, afterMod)
	require.NoFileExists(t, filepath.Join(tmp, toolFileCanonical))
}

func TestPinLockedProcessesYAMLAndUnconfiguredModules(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	yamlRoot := filepath.Join(tmp, "yaml-root")
	unconfiguredRoot := filepath.Join(tmp, "unconfigured-root")
	require.NoError(t, os.MkdirAll(yamlRoot, 0o755))
	require.NoError(t, os.MkdirAll(unconfiguredRoot, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(yamlRoot, goModFileName),
		[]byte("module example.com/yaml-root\n\ngo 1.25\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(yamlRoot, instrumentationYAMLCanonical),
		[]byte("instrumentations: []\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(unconfiguredRoot, goModFileName),
		[]byte("module example.com/unconfigured-root\n\ngo 1.25\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(unconfiguredRoot, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"),
		0o644,
	))

	result, err := pinLocked(t.Context(), PinOptions{ModuleDirs: []string{
		yamlRoot,
		unconfiguredRoot,
	}})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.AllDeps)
	require.NoFileExists(t, filepath.Join(yamlRoot, toolFileCanonical))
	require.FileExists(t, filepath.Join(unconfiguredRoot, toolFileCanonical))
}

func TestAutoPinYAMLDoesNotModifyYAML(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	invalid := filepath.Join(tmp, "invalid")
	require.NoError(t, os.MkdirAll(invalid, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(tmp, "go.mod"),
		fmt.Appendf(nil, `module example.com/root

go 1.25

require example.com/invalid v0.0.0

replace example.com/invalid => %s
`, invalid),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(invalid, "go.mod"), []byte("module example.com/invalid\n\ngo 1.25\n"), 0o644,
	))
	require.NoError(t, os.WriteFile(filepath.Join(invalid, "invalid.go"), []byte("package invalid\n"), 0o644))
	yamlFile := filepath.Join(tmp, instrumentationYAMLCanonical)
	yaml := []byte("instrumentations:\n  - example.com/invalid\n")
	require.NoError(t, os.WriteFile(yamlFile, yaml, 0o644))

	sm := newStateManager()
	ctx := contextWithStateManager(t.Context(), sm)
	_, err := autoPin(ctx, []string{tmp}, subcmdBuild, nil)
	require.NoError(t, err)
	afterYAML, err := os.ReadFile(yamlFile)
	require.NoError(t, err)
	require.YAMLEq(t, string(yaml), string(afterYAML))
	toolData, err := os.ReadFile(filepath.Join(tmp, toolFileCanonical))
	require.NoError(t, err)
	require.NotContains(t, string(toolData), "example.com/invalid")
}

func TestAutoPinEmptyYAMLSuppressesDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.25\n"), 0o644))
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(dir, "main.go"),
			[]byte("package main\nimport _ \"net/http\"\nfunc main() {}\n"),
			0o644,
		),
	)
	yamlFile := filepath.Join(dir, instrumentationYAMLCanonical)
	original := []byte("# deliberately disabled\ninstrumentations: []\n")
	require.NoError(t, os.WriteFile(yamlFile, original, 0o644))
	ctx := contextWithStateManager(t.Context(), newStateManager())
	_, err := autoPin(ctx, []string{dir}, subcmdBuild, []string{"."})
	require.NoError(t, err)
	toolFile, err := os.ReadFile(filepath.Join(dir, toolFileCanonical))
	require.NoError(t, err)
	require.NotContains(t, string(toolFile), "go.opentelemetry.io/otelc/instrumentation/net/http")
	require.NotContains(t, string(toolFile), "import (")
	got, err := os.ReadFile(yamlFile)
	require.NoError(t, err)
	require.Equal(t, original, got)
}

func TestAutoPinMissingCheckoutDoesNotSnapshotModule(t *testing.T) {
	for _, withYAML := range []bool{true, false} {
		name := "built_in_defaults"
		if withYAML {
			name = "yaml"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv(util.EnvOtelcWorkDir, dir)
			t.Setenv("OTELC_SOURCE_ROOT", t.TempDir())
			t.Setenv("GOWORK", "off")
			t.Chdir(dir)
			mod := []byte("module example.com/app\n\ngo 1.25\n")
			require.NoError(t, os.WriteFile(filepath.Join(dir, goModFileName), mod, 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
				[]byte("package main\nimport _ \"net/http\"\nfunc main() {}\n"), 0o644))
			if withYAML {
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, instrumentationYAMLCanonical),
					[]byte(
						"instrumentations:\n  - go.opentelemetry.io/otelc/instrumentation/net/http/client\n",
					),
					0o644,
				))
			}
			ctx := contextWithStateManager(t.Context(), newStateManager())
			_, err := autoPin(ctx, []string{dir}, subcmdBuild, []string{"."})
			require.ErrorContains(t, err, "otelc source checkout not found")
			require.NoFileExists(t, util.GetBuildTemp(stateFileName), "no user files should have been snapshotted")
			require.NoFileExists(t, filepath.Join(dir, toolFileCanonical))
			got, err := os.ReadFile(filepath.Join(dir, goModFileName))
			require.NoError(t, err)
			require.Equal(t, mod, got)
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
					Target:       rule.NewTarget("example.com/foo"),
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
					Target:       rule.NewTarget("example.com/bar"),
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
					Target:       rule.NewTarget("example.com/foo"),
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
					Target:       rule.NewTarget("example.com/foo"),
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
					Target:       rule.NewTarget("example.com/foo"),
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
					Target:       rule.NewTarget("example.com/*"),
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
					Target:       rule.NewTarget("example.com/*"),
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
					Target: rule.NewTarget(rule.TargetRoot),
				}},
			},
			want: map[string]bool{
				"example.com/instrumentation/foo": true,
			},
		},
		{
			name: "target list including root",
			deps: []*Dependency{{ImportPath: "example.com/foo"}},
			rules: map[string][]yamlRule{
				"example.com/instrumentation/foo": {{
					Target: rule.NewTarget(rule.TargetRoot, "main"),
				}},
			},
			want: map[string]bool{"example.com/instrumentation/foo": true},
		},
		{
			name: "target list",
			deps: []*Dependency{{ImportPath: "example.com/bar"}},
			rules: map[string][]yamlRule{
				"example.com/instrumentation/foo": {{
					Target: rule.NewTarget("example.com/foo", "example.com/bar"),
				}},
			},
			want: map[string]bool{"example.com/instrumentation/foo": true},
		},
		{
			name: "target list excluding the dependency",
			deps: []*Dependency{{ImportPath: "example.com/foo/mock"}},
			rules: map[string][]yamlRule{
				"example.com/instrumentation/foo": {{
					Target: rule.Target{Include: []string{"example.com/foo/**"}, Exclude: []string{"example.com/foo/mock"}},
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
					Target:       rule.NewTarget("example.com/foo"),
					VersionRange: "v1.0.0",
				}},
				"example.com/instrumentation/bar": {{
					Target:       rule.NewTarget("example.com/bar"),
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
				Target:       rule.NewTarget("example.com/foo"),
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
				{Target: rule.NewTarget("example.com/foo/v1"), VersionRange: "v1.0.0"},
				{Target: rule.NewTarget("example.com/foo/v1/sub"), VersionRange: ""},
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
				{Target: rule.NewTarget("example.com/foo"), VersionRange: "v1.0.0"},
				{Target: rule.NewTarget("example.com/foo"), VersionRange: "v2.0.0"},
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
version: "v1.0.0"
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
version: "v1.0.0"
ruleNested:
  target: example.com/nested-target
  version: v1.0.0
`), 0o644))

	rules, err := loadMinimalRules(t.Context(), dir, util.Version)
	require.NoError(t, err)

	// make sure only 2 rules are loaded (sub1 and nested, sub1 doesn't load nested rules)
	require.Len(t, rules, 2)
	require.Contains(t, rules, "example.com/sub1")
	require.Contains(t, rules, "example.com/sub1/nested")

	require.Len(t, rules["example.com/sub1"], 1)
	require.Equal(t, "example.com/target", rules["example.com/sub1"][0].Target.String())
	require.Equal(t, "v1.0.0", rules["example.com/sub1"][0].VersionRange)

	require.Len(t, rules["example.com/sub1/nested"], 1)
	require.Equal(t, "example.com/nested-target", rules["example.com/sub1/nested"][0].Target.String())
}

func TestLoadMinimalRulesMinimumVersionMetadata(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "module")
	require.NoError(t, os.Mkdir(moduleDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"),
		[]byte("module example.com/module\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "otelc.yaml"), []byte(`
version: "v1.1.0"
rule:
  target: example.com/target
  version: v2.0.0,v3.0.0
`), 0o644))

	rules, err := loadMinimalRules(t.Context(), dir, util.Version)
	require.NoError(t, err)
	require.Len(t, rules["example.com/module"], 1)
	assert.Equal(t, "example.com/target", rules["example.com/module"][0].Target.String())
	assert.Equal(t, "v2.0.0,v3.0.0", rules["example.com/module"][0].VersionRange)
}

func TestLoadMinimalRulesRejectsNewerOtelcVersion(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "module")
	require.NoError(t, os.Mkdir(moduleDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"),
		[]byte("module example.com/module\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "otelc.yaml"), []byte(`
version: "v1.1.0"
rule:
  target: example.com/target
`), 0o644))

	_, err := loadMinimalRules(t.Context(), dir, "v1.0.0")
	require.ErrorContains(t, err, "requires otelc >= v1.1.0")
}

func TestLoadMinimalRulesWarnsForLegacyFile(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "module")
	require.NoError(t, os.Mkdir(moduleDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"),
		[]byte("module example.com/module\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "otelc.yaml"), []byte(`
rule:
  target: example.com/target
`), 0o644))

	var output strings.Builder
	logger := slog.New(slog.NewTextHandler(&output, nil))
	ctx := util.ContextWithLogger(t.Context(), logger)
	_, err := loadMinimalRules(ctx, dir, "v1.0.0")
	require.NoError(t, err)
	assert.Contains(t, output.String(), "no minimum otelc version")
	assert.Contains(t, output.String(), "otelc.yaml")
}

func TestLoadMinimalRules_InvalidGoMod(t *testing.T) {
	dir := t.TempDir()

	sub1 := filepath.Join(dir, "sub1")
	require.NoError(t, os.Mkdir(sub1, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub1, "go.mod"), []byte("invalid"), 0o644))

	_, err := loadMinimalRules(t.Context(), dir, util.Version)
	require.Error(t, err)
}

func TestLoadMinimalRules_InvalidRuleYAML(t *testing.T) {
	dir := t.TempDir()

	sub1 := filepath.Join(dir, "sub1")
	require.NoError(t, os.Mkdir(sub1, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub1, "go.mod"), []byte("module example.com/sub1\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sub1, "otelc.yaml"), []byte("invalid: yaml: {"), 0o644))

	_, err := loadMinimalRules(t.Context(), dir, util.Version)
	require.Error(t, err)
}

func TestLoadMinimalRules_InvalidTarget(t *testing.T) {
	dir := t.TempDir()

	sub1 := filepath.Join(dir, "sub1")
	require.NoError(t, os.Mkdir(sub1, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub1, "go.mod"), []byte("module example.com/sub1\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sub1, "otelc.yaml"), []byte(`
version: "v1.0.0"
rule1:
  target:
    - not: example.com/target
`), 0o644))

	_, err := loadMinimalRules(t.Context(), dir, util.Version)
	require.ErrorContains(t, err, "selects no package")
}

func TestValidateRuleFiles(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "otelc.yaml")
		require.NoError(t, os.WriteFile(path, []byte(`version: "v1.0.0"
rule:
  target: main
  func: Example
  raw: "_ = 1"
`), 0o644))

		require.NoError(t, validateRuleFiles(t.Context(), []string{path}))
	})

	t.Run("read error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.otelc.yaml")
		err := validateRuleFiles(t.Context(), []string{path})
		require.ErrorContains(t, err, "reading")
		require.ErrorContains(t, err, path)
	})

	t.Run("invalid metadata", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "otelc.yaml")
		require.NoError(t, os.WriteFile(path, []byte(`version: 1`), 0o644))

		err := validateRuleFiles(t.Context(), []string{path})
		require.ErrorContains(t, err, "minimum otelc version must be a string")
	})

	t.Run("invalid rule", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "otelc.yaml")
		require.NoError(t, os.WriteFile(path, []byte(`version: "v1.0.0"
rule: value
`), 0o644))

		err := validateRuleFiles(t.Context(), []string{path})
		require.Error(t, err)
	})
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

	toolFile := filepath.Join(dir, toolFileCanonical)

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

func TestUpdateToolFile_SteadyStateSkipsModTidy(t *testing.T) {
	trueValue := true

	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte(`module example.com/test

go 1.25
`),
		0o644,
	))

	toolFile := filepath.Join(dir, toolFileCanonical)
	writeToolFile(t, toolFile, "fmt")

	opts := PinOptions{
		Prune:    true,
		Generate: &trueValue,
	}

	// First run canonicalizes the tool file and adds the otelc require.
	require.NoError(t, updateToolFile(t.Context(), toolFile, nil, opts))

	toolFileAfterFirst, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	goModAfterFirst, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)

	// Second run changes nothing, so go mod tidy must be skipped.
	var logs bytes.Buffer
	debugLogger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := util.ContextWithLogger(t.Context(), debugLogger)

	require.NoError(t, updateToolFile(ctx, toolFile, nil, opts))
	require.Contains(t, logs.String(), skipTidyMessage)

	toolFileAfterSecond, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	require.Equal(t, string(toolFileAfterFirst), string(toolFileAfterSecond))

	goModAfterSecond, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	require.Equal(t, string(goModAfterFirst), string(goModAfterSecond))
}

func TestUpdateToolFile_MissingGoSumRunsTidy(t *testing.T) {
	trueValue := true

	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte(`module example.com/test

go 1.25
`),
		0o644,
	))

	toolFile := filepath.Join(dir, toolFileCanonical)
	writeToolFile(t, toolFile, "fmt")

	opts := PinOptions{
		Prune:    true,
		Generate: &trueValue,
	}

	require.NoError(t, updateToolFile(t.Context(), toolFile, nil, opts))

	// A hand-deleted go.sum must force a tidy even when nothing else changed.
	goSumPath := filepath.Join(dir, "go.sum")
	require.NoError(t, os.Remove(goSumPath))

	var logs bytes.Buffer
	debugLogger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := util.ContextWithLogger(t.Context(), debugLogger)

	require.NoError(t, updateToolFile(ctx, toolFile, nil, opts))
	require.NotContains(t, logs.String(), skipTidyMessage)
	require.FileExists(t, goSumPath, "go mod tidy should have restored go.sum")
}

func TestUpdateToolFile_SkippedTidyKeepsManualRequire(t *testing.T) {
	trueValue := true

	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte(`module example.com/test

go 1.25
`),
		0o644,
	))

	toolFile := filepath.Join(dir, toolFileCanonical)
	writeToolFile(t, toolFile, "fmt")

	opts := PinOptions{
		Prune:    true,
		Generate: &trueValue,
	}

	// First run reaches the steady state.
	require.NoError(t, updateToolFile(t.Context(), toolFile, nil, opts))

	goModPath := filepath.Join(dir, "go.mod")
	goModAfterFirst, err := os.ReadFile(goModPath)
	require.NoError(t, err)

	// The user adds a manual require. otelc owns only the lines it
	// writes, so the manual require must survive the skipped tidy.
	goModManual := string(goModAfterFirst) + "\nrequire example.com/manual v1.2.3\n"
	require.NoError(t, os.WriteFile(goModPath, []byte(goModManual), 0o644))

	var logs bytes.Buffer
	debugLogger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := util.ContextWithLogger(t.Context(), debugLogger)

	require.NoError(t, updateToolFile(ctx, toolFile, nil, opts))
	require.Contains(t, logs.String(), skipTidyMessage)

	goModAfterSecond, err := os.ReadFile(goModPath)
	require.NoError(t, err)
	require.Equal(t, goModManual, string(goModAfterSecond))
}

func TestUpdateToolFile_PruneAfterSteadyStateRunsTidy(t *testing.T) {
	trueValue := true

	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte(`module example.com/test

go 1.25
`),
		0o644,
	))

	toolFile := filepath.Join(dir, toolFileCanonical)
	writeToolFile(t, toolFile, "fmt")

	opts := PinOptions{
		Prune:    true,
		Generate: &trueValue,
	}

	// First run reaches the steady state.
	require.NoError(t, updateToolFile(t.Context(), toolFile, nil, opts))

	var logs bytes.Buffer
	debugLogger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := util.ContextWithLogger(t.Context(), debugLogger)

	// A prune must flip toolFileChanged and force the tidy.
	require.NoError(t, updateToolFile(ctx, toolFile, map[string]bool{"fmt": true}, opts))
	require.NotContains(t, logs.String(), skipTidyMessage)

	toolFileAfter, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	require.NotContains(t, string(toolFileAfter), `"fmt"`)
}

func TestEnsureOtelcRequire_DevVersionReportsMissingRequire(t *testing.T) {
	dir := t.TempDir()

	// Tool directive present, require line absent: the state a dev build
	// leaves behind, since ensureOtelcRequireVersion will not pin v0.0.0 or a
	// pseudo-version and so cannot add the require itself.
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, goModFileName),
		[]byte(`module example.com/test

go 1.25

tool go.opentelemetry.io/otelc/tool/cmd/otelc
`),
		0o644,
	))

	for _, version := range []string{"v0.0.0", "v0.0.0-20260101000000-000000000000", "(devel)"} {
		t.Run(version, func(t *testing.T) {
			modified, err := ensureOtelcRequire(dir, version)
			require.NoError(t, err)
			require.True(t, modified, "a missing require must be reported so the caller still tidies")
		})
	}
}

func TestUpdateToolFile_ReadError(t *testing.T) {
	err := updateToolFile(t.Context(),
		filepath.Join(t.TempDir(), "does-not-exist.go"),
		nil,
		PinOptions{},
	)

	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestUpdateToolFile_ParseError(t *testing.T) {
	toolFile := filepath.Join(t.TempDir(), toolFileCanonical)
	require.NoError(t, os.WriteFile(toolFile, []byte("this is not go"), 0o644))

	err := updateToolFile(t.Context(), toolFile, nil, PinOptions{})

	require.ErrorContains(t, err, "failed to parse file")
}

func TestUpdateToolFile_WriteError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod permissions are not enforced consistently on Windows")
	}

	dir := t.TempDir()
	toolFile := filepath.Join(dir, toolFileCanonical)
	writeToolFile(t, toolFile, "fmt")

	original, err := os.ReadFile(toolFile)
	require.NoError(t, err)

	// A read-only directory still lets updateToolFile read the tool file, but
	// not replace it, since the atomic write needs a temp file next to it.
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
	})

	// Pruning "fmt" changes the tool file, so updateToolFile has to write it.
	err = updateToolFile(t.Context(), toolFile, map[string]bool{"fmt": true}, PinOptions{Prune: true})
	require.ErrorContains(t, err, "failed to create temporary file")

	after, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	require.Equal(t, string(original), string(after))
}

func TestUpdateToolFile_EnsureRequireError(t *testing.T) {
	dir := t.TempDir()

	// valid tool file
	writeToolFile(t, filepath.Join(dir, toolFileCanonical), "fmt")

	// intentionally invalid go.mod
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("not a go mod"),
		0o644,
	))

	err := updateToolFile(t.Context(),
		filepath.Join(dir, toolFileCanonical),
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
		filepath.Join(root, toolFileCanonical),
		"example.com/foo",
	)

	err := updatePinnedProjects(
		t.Context(),
		[]string{filepath.Join(root, toolFileCanonical)},
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
		[]string{dir},
		PinOptions{},
	)
	require.NoError(t, err)

	// syncDeps should have run, so PinResult should be empty.
	require.NotNil(t, result)
	require.Nil(t, result.AllDeps)

	toolFile := filepath.Join(dir, toolFileCanonical)

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

func TestGeneratePinnedProjectsSecondModuleFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, root)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	repositoryDir, err := repositorySourceRoot()
	require.NoError(t, err)
	t.Setenv("OTELC_SOURCE_ROOT", repositoryDir)
	t.Chdir(root)
	first := filepath.Join(root, "a")
	second := filepath.Join(root, "z")
	require.NoError(t, os.MkdirAll(first, 0o755))
	require.NoError(t, os.MkdirAll(second, 0o755))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n\ngo 1.25\n"), 0o644),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(root, "main.go"),
			[]byte("package main\nimport _ \"net/http\"\nfunc main() {}\n"),
			0o644,
		),
	)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(first, "go.mod"), []byte("module example.com/first\n\ngo 1.25\n"), 0o644),
	)
	require.NoError(t, os.WriteFile(filepath.Join(second, "go.mod"), []byte("invalid go.mod"), 0o644))
	_, err = generatePinnedProjects(
		t.Context(),
		[]string{first, second},
		PinOptions{Args: []string{"."}},
	)
	require.Error(t, err)
	require.ErrorContains(t, err, filepath.Join(second, goModFileName))
	require.FileExists(t, filepath.Join(first, toolFileCanonical), "the first module was already materialized")
	require.FileExists(t, filepath.Join(second, toolFileCanonical), "the second write precedes go.mod validation")
}

func TestGeneratePinnedProjectsTwoModules(t *testing.T) {
	root := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, root)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	repositoryDir, err := repositorySourceRoot()
	require.NoError(t, err)
	t.Setenv("OTELC_SOURCE_ROOT", repositoryDir)
	t.Chdir(root)
	require.NoError(t, os.WriteFile(filepath.Join(root, goModFileName),
		[]byte("module example.com/test\n\ngo 1.25\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"),
		[]byte("package main\nimport _ \"net/http\"\nfunc main() {}\n"), 0o644))
	first := filepath.Join(root, "a")
	second := filepath.Join(root, "z")
	for _, moduleDir := range []string{first, second} {
		require.NoError(t, os.MkdirAll(moduleDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(moduleDir, goModFileName),
			[]byte("module example.com/"+filepath.Base(moduleDir)+"\n\ngo 1.25\n"), 0o644))
	}

	result, err := generatePinnedProjects(
		t.Context(),
		[]string{first, second},
		PinOptions{Args: []string{"."}},
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.AllDeps)
	for _, moduleDir := range []string{first, second} {
		toolFile, readErr := os.ReadFile(filepath.Join(moduleDir, toolFileCanonical))
		require.NoError(t, readErr)
		require.Contains(t, string(toolFile), "go.opentelemetry.io/otelc/instrumentation/net/http/client")
		goMod, readErr := os.ReadFile(filepath.Join(moduleDir, goModFileName))
		require.NoError(t, readErr)
		require.Contains(t, string(goMod), "tool go.opentelemetry.io/otelc/tool/cmd/otelc")
	}
}

func TestGeneratePinnedProjectsNoMatchingInstrumentation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, dir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	t.Setenv("GOWORK", "off")
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, goModFileName),
		[]byte("module example.com/empty\n\ngo 1.25\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.go"),
		[]byte("package empty\nfunc Example() {}\n"), 0o644))
	result, err := generatePinnedProjects(t.Context(), []string{dir}, PinOptions{Args: []string{"."}})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.AllDeps)
	require.NoFileExists(t, filepath.Join(dir, toolFileCanonical))
}

func TestGeneratePinnedProjectsFindDepsError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := generatePinnedProjects(ctx, []string{t.TempDir()}, PinOptions{})
	require.Error(t, err)
}

func TestPrepareVendoredBuild_NotVendored(t *testing.T) {
	// A plain module with no vendor/ directory must be left untouched: the
	// args come back verbatim and GOFLAGS is not forced to module mode.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"),
		0o644,
	))
	t.Chdir(dir)
	t.Setenv(util.EnvOtelcWorkDir, dir)
	t.Setenv("GOFLAGS", "")

	args := []string{"build", "-mod=vendor", "./..."}
	got, err := prepareVendoredBuild(t.Context(), discardLogger(), subcmdBuild, args)
	require.NoError(t, err)

	// Unchanged: not a vendored project, so no rewriting happens.
	assert.Equal(t, args, got)
	assert.Empty(t, os.Getenv("GOFLAGS"))
}

func TestPrepareVendoredBuild_Vendored(t *testing.T) {
	// A module that vendors its dependencies must be switched to module mode:
	// GOFLAGS gains -mod=mod and an explicit -mod=vendor on the command line
	// is rewritten so it cannot re-select vendoring.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"),
		0o644,
	))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "vendor"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "vendor", "modules.txt"),
		[]byte(""),
		0o644,
	))
	t.Chdir(dir)
	t.Setenv(util.EnvOtelcWorkDir, dir)
	t.Setenv("GOFLAGS", "")
	// Force non-workspace mode; -mod=mod is forbidden in a workspace, so a
	// stray ambient go.work would otherwise suppress vendoring detection.
	t.Setenv("GOWORK", "off")

	args := []string{"build", "-mod=vendor", "./..."}
	got, err := prepareVendoredBuild(t.Context(), discardLogger(), subcmdBuild, args)
	require.NoError(t, err)

	assert.Equal(t, []string{"build", "-mod=mod", "./..."}, got)
	assert.Contains(t, os.Getenv("GOFLAGS"), "-mod=mod")
}

func TestPinLocked_UpdatesExistingToolFile(t *testing.T) {
	// With ModuleDirs supplied, pinLocked skips dependency discovery and goes
	// straight to updating the existing tool file. A dependency that turns out
	// not to be an instrumentation package must be pruned from it.
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

	_, err := pinLocked(t.Context(), PinOptions{
		Prune:      true,
		ModuleDirs: []string{tmp},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "example.com/notinstrumentation")
}

func TestPinLocked_UnsortedDuplicateModuleDirs(t *testing.T) {
	// Assert that unsorted, duplicate PinOptions.ModuleDirs are normalized
	// so each module is processed exactly once, and the caller's slice is not mutated.
	tmp := t.TempDir()

	notInstDirA := filepath.Join(tmp, "notiA")
	writeInstrumentationModule(
		t,
		notInstDirA,
		"example.com/notiA",
		false,
		nil,
	)

	notInstDirB := filepath.Join(tmp, "notiB")
	writeInstrumentationModule(
		t,
		notInstDirB,
		"example.com/notiB",
		false,
		nil,
	)

	modA := filepath.Join(tmp, "modA")
	modB := filepath.Join(tmp, "modB")

	toolFileA := writeInstrumentationModule(t, modA, "example.com/modA", false, map[string]string{
		"example.com/notiA": notInstDirA,
	})
	toolFileB := writeInstrumentationModule(t, modB, "example.com/modB", false, map[string]string{
		"example.com/notiB": notInstDirB,
	})

	var logOutput strings.Builder
	logger := slog.New(slog.NewTextHandler(&logOutput, nil))
	ctx := util.ContextWithLogger(t.Context(), logger)

	callerDirs := []string{modB, modA, modB, modA}
	callerOrig := append([]string(nil), callerDirs...)

	result, err := pinLocked(ctx, PinOptions{
		Prune:      true,
		ModuleDirs: callerDirs,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Caller slice must remain unchanged
	assert.Equal(t, callerOrig, callerDirs, "caller slice must not be mutated")

	// Both tool files must be updated and uninstrumented imports pruned
	dataA, err := os.ReadFile(toolFileA)
	require.NoError(t, err)
	assert.NotContains(t, string(dataA), "example.com/notiA")

	dataB, err := os.ReadFile(toolFileB)
	require.NoError(t, err)
	assert.NotContains(t, string(dataB), "example.com/notiB")

	// Each module should have been processed exactly once
	assert.Equal(t, 1, strings.Count(logOutput.String(), toolFileA), "modA should be processed exactly once")
	assert.Equal(t, 1, strings.Count(logOutput.String(), toolFileB), "modB should be processed exactly once")
}

func TestPinLocked_DiscoversModuleDirs(t *testing.T) {
	// With no ModuleDirs supplied, pinLocked must discover them from the build
	// packages in the working directory. With no existing tool file, it falls
	// through to generating one from the dependency graph.
	dir := t.TempDir()
	setPinSourceRoot(t)
	t.Setenv(util.EnvOtelcWorkDir, dir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755)) // ensure .otelc-build exists
	t.Chdir(dir)

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "main.go"),
		[]byte("package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hi\") }\n"),
		0o644,
	))

	_, err := pinLocked(t.Context(), PinOptions{})
	require.NoError(t, err)

	// A tool file is generated for the discovered module.
	require.FileExists(t, filepath.Join(dir, toolFileCanonical))
}

// newPinRoot is newBuildRoot for a standalone pin, which keeps its work files
// in the build root.
func newPinRoot(t *testing.T, files map[string]string) string {
	t.Helper()
	setPinSourceRoot(t)
	root := newBuildRoot(t, files)
	t.Setenv(util.EnvOtelcWorkDir, root)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	return root
}

func setPinSourceRoot(t *testing.T) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	t.Setenv("OTELC_SOURCE_ROOT", filepath.Clean(filepath.Join(filepath.Dir(file), "../../..")))
}

func TestPinLocked_StandaloneToolFile(t *testing.T) {
	// The golden path is relative to the working directory, which the
	// subtests change, so make it absolute first.
	goldenFile, absErr := filepath.Abs(filepath.Join("testdata", "standalone.otel.instrumentation.go.golden"))
	require.NoError(t, absErr)

	tests := []struct {
		name  string
		files map[string]string
		args  []string
		want  []string // dirs with a tool file, relative to the build root
	}{
		{
			name:  "-C flag",
			files: changeDirFiles(),
			args:  []string{"-C", "app", "."},
			want:  []string{"app"},
		},
		{
			name:  "go.work with two modules",
			files: workspaceFiles(),
			args:  []string{"./a", "./b"},
			want:  []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newPinRoot(t, tt.files)

			_, err := pinLocked(t.Context(), PinOptions{Prune: true, Args: tt.args})
			require.NoError(t, err)

			for _, dir := range tt.want {
				actual, readErr := os.ReadFile(filepath.Join(root, dir, toolFileCanonical))
				require.NoError(t, readErr)

				actualNorm := strings.ReplaceAll(string(actual), "\r\n", "\n")
				golden.Assert(t, actualNorm, goldenFile)
			}
		})
	}
}

func TestPinLocked_ImportCycleRoot(t *testing.T) {
	// Finding the module dirs does not resolve imports, so a root package in
	// an import cycle is not skipped there. A fresh pin fails later, with the
	// error of the go command; an already pinned module is updated as usual.
	tests := []struct {
		name        string
		pinnedFirst bool
		wantErr     string
	}{
		{name: "fresh pin", wantErr: "import cycle not allowed"},
		{name: "already pinned", pinnedFirst: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newPinRoot(t, plainModuleFiles())
			toolFile := filepath.Join(root, toolFileCanonical)

			var pinned []byte
			if tt.pinnedFirst {
				_, err := pinLocked(t.Context(), PinOptions{Prune: true})
				require.NoError(t, err)
				pinned, err = os.ReadFile(toolFile)
				require.NoError(t, err)
			}

			mustWriteFile(t, filepath.Join(root, "a", "a.go"), "package a\n\nimport _ \"example.com/app/b\"\n")
			mustWriteFile(t, filepath.Join(root, "b", "b.go"), "package b\n\nimport _ \"example.com/app/a\"\n")

			_, err := pinLocked(t.Context(), PinOptions{Prune: true, Args: []string{"./a"}})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.NoFileExists(t, toolFile)
				return
			}
			require.NoError(t, err)
			actual, err := os.ReadFile(toolFile)
			require.NoError(t, err)
			assert.Equal(t, string(pinned), string(actual), "tool file must not change")
		})
	}
}

func TestPin_UpdatesExistingToolFile(t *testing.T) {
	// Pin wraps pinLocked under the build lock. Point the work dir at the
	// module so the lock is taken in the sandbox rather than an ambient path.
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)

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

	result, err := Pin(t.Context(), PinOptions{
		Prune:      true,
		ModuleDirs: []string{tmp},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "example.com/notinstrumentation")
}

func TestAutoPin_NoStateManager(t *testing.T) {
	// autoPin cannot track files to restore without a stateManager in context.
	_, err := autoPin(t.Context(), []string{t.TempDir()}, subcmdBuild, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state manager not found")
}

func TestAutoPin_TracksAndPins(t *testing.T) {
	// With a stateManager present, autoPin backs up the mutable files, tracks
	// them, then pins — pruning the non-instrumentation dependency along the way.
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755)) // ensure .otelc-build exists for state snapshots

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

	sm := newStateManager()
	ctx := contextWithStateManager(t.Context(), sm)

	_, err := autoPin(ctx, []string{tmp}, subcmdBuild, nil)
	require.NoError(t, err)

	// getBackupFiles tracks go.mod, go.sum, and the tool file together for
	// every module directory; assert all three, not just go.mod, so a
	// regression that drops one of them from the backup set is caught.
	for _, name := range []string{"go.mod", "go.sum", toolFileCanonical} {
		abs, absErr := filepath.Abs(filepath.Join(tmp, name))
		require.NoError(t, absErr)
		assert.Contains(t, sm.files, filepath.Clean(abs),
			"expected %s to be tracked by the state manager", name)
	}

	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "example.com/notinstrumentation")
}

// newModuleDir creates a minimal Go module in a fresh temp directory and
// returns its path.
func newModuleDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/vend\n\ngo 1.25\n"),
		0o644,
	))
	return dir
}

func TestPrepareVendoredBuild(t *testing.T) {
	// With no vendored module active, prepareVendoredBuild returns the args
	// unchanged and does not force module mode.
	dir := newModuleDir(t)
	t.Chdir(dir)
	t.Setenv(util.EnvOtelcWorkDir, dir)

	args := []string{"build", "./..."}
	got, err := prepareVendoredBuild(
		context.Background(),
		util.LoggerFromContext(context.Background()),
		subcmdBuild,
		args,
	)
	require.NoError(t, err)
	assert.Equal(t, args, got)
}

func TestPinLocked_GetBuildPackagesError(t *testing.T) {
	_, err := Pin(t.Context(), PinOptions{
		Args: []string{"-o"}, // missing required flag value
	})
	require.Error(t, err)
}

func TestAutoPin_TrackAllError(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	modDir := filepath.Join(tmp, "mod")
	require.NoError(t, os.MkdirAll(modDir, 0o755))
	mustWriteFile(t, filepath.Join(modDir, "go.mod"), "module example.com")

	// Snapshot destination dir as file so TrackAll fails on all platforms (including Windows)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	snapshotDir := util.GetBuildTemp(stateDir)
	_ = os.RemoveAll(snapshotDir)
	require.NoError(t, os.WriteFile(snapshotDir, []byte("file"), 0o644))

	sm := newStateManager()
	ctx := contextWithStateManager(t.Context(), sm)

	_, err := autoPin(ctx, []string{modDir}, "build", []string{"."})
	require.Error(t, err)
}

func TestRemoveImports_UnquoteError(t *testing.T) {
	f := &dst.File{
		Decls: []dst.Decl{
			&dst.GenDecl{
				Tok: token.IMPORT,
				Specs: []dst.Spec{
					&dst.ImportSpec{
						Path: &dst.BasicLit{
							Kind:  token.STRING,
							Value: `unquoted"invalid`,
						},
					},
				},
			},
		},
	}
	err := removeImports(f, map[string]bool{"foo": true})
	require.Error(t, err)
}

func TestAutoPin_GetBackupFilesError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // canceled context causes getBackupFiles to fail

	sm := newStateManager()
	ctx = contextWithStateManager(ctx, sm)

	_, err := autoPin(ctx, []string{"/some/dir"}, "build", []string{"."})
	require.Error(t, err)
}

func TestGeneratePinnedProjects_FindDepsError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // canceled context causes findDeps to fail

	_, err := generatePinnedProjects(ctx, []string{"/some/dir"}, PinOptions{})
	require.Error(t, err)
}

func TestPinLocked_FindModuleDirsError(t *testing.T) {
	// A standalone .go file outside any Go module causes FindModuleDirs in pinLocked to fail on line 651
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)

	mainFile := filepath.Join(tmp, "main.go")
	mustWriteFile(t, mainFile, "package main\nfunc main() {}\n")

	_, err := Pin(t.Context(), PinOptions{Args: []string{mainFile}})
	require.Error(t, err)
}

func TestGeneratePinnedProjects_SyncDepsError(t *testing.T) {
	goMod := `module example.com/test

go 1.21

require (
	go.opentelemetry.io/otelc v0.0.0
	nonexistent.invalid/pkg v1.0.0
)
`
	tempDir, repositoryDir, _ := setupSyncDepsTest(t, goMod, []string{"net/http/client"})
	t.Setenv("OTELC_SOURCE_ROOT", repositoryDir)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(tempDir, "main.go"),
			[]byte("package main\nimport _ \"net/http\"\nfunc main() {}\n"),
			0o644,
		),
	)
	ruleFile := filepath.Join(repositoryDir, unzippedInstDir, "net", "http", "client", "rules.yaml")
	require.NoError(t, os.WriteFile(ruleFile, []byte("rule1:\n  target: net/http\n  func: Get\n"), 0o644))

	_, err := generatePinnedProjects(t.Context(), []string{tempDir}, PinOptions{
		Args: []string{"."},
	})
	require.Error(t, err)
}

func TestGeneratePinnedProjects_EnsureOtelcRequireError(t *testing.T) {
	goMod := `module example.com/test

go 1.21
`
	tempDir, repositoryDir, _ := setupSyncDepsTest(t, goMod, []string{"net/http/client"})
	t.Setenv("OTELC_SOURCE_ROOT", repositoryDir)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(tempDir, "main.go"),
			[]byte("package main\nimport _ \"net/http\"\nfunc main() {}\n"),
			0o644,
		),
	)
	ruleFile := filepath.Join(repositoryDir, unzippedInstDir, "net", "http", "client", "rules.yaml")
	require.NoError(t, os.WriteFile(ruleFile, []byte("rule1:\n  target: net/http\n  func: Get\n"), 0o644))

	// Corrupt go.mod so ensureOtelcRequire fails in generatePinnedProjects (line 563)
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("invalid go.mod {"), 0o644))

	_, err := generatePinnedProjects(t.Context(), []string{tempDir}, PinOptions{})
	require.Error(t, err)
}

func TestUpdateToolFile_RemoveImportsError(t *testing.T) {
	tempDir := t.TempDir()
	toolFile := filepath.Join(tempDir, "otel.instrumentation.go")
	content := "package main\n\nimport _ `pkg\nnewline`\n"
	require.NoError(t, os.WriteFile(toolFile, []byte(content), 0o644))

	err := updateToolFile(t.Context(), toolFile, map[string]bool{"foo": true}, PinOptions{})
	require.Error(t, err)
}
