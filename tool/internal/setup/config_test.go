// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

func TestFindToolFile(t *testing.T) {
	for _, tt := range []struct {
		name    string
		setup   func(string)
		want    string
		wantErr bool
	}{
		{
			name:  "none",
			setup: func(string) {},
		},
		{
			name: "canonical",
			setup: func(dir string) {
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, ToolFileCanonical),
					nil,
					0o644,
				))
			},
			want: ToolFileCanonical,
		},
		{
			name: "alias",
			setup: func(dir string) {
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, ToolFileAlias),
					nil,
					0o644,
				))
			},
			want: ToolFileAlias,
		},
		{
			name: "both",
			setup: func(dir string) {
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, ToolFileCanonical),
					nil,
					0o644,
				))
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, ToolFileAlias),
					nil,
					0o644,
				))
			},
			wantErr: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(dir)

			got, err := findToolFile(dir)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.want != "" {
				require.Equal(t, filepath.Join(dir, tt.want), got)
			} else {
				require.Empty(t, got)
			}
		})
	}
}

func TestResolveInstrumentationConfig(t *testing.T) {
	tests := []struct {
		name       string
		importPath string
		setup      func(t *testing.T, dir string)
		wantErr    bool
		wantTool   string
		wantRules  []string
	}{
		{
			name:       "tool file only",
			importPath: "example.com/test",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "go.mod"),
					[]byte("module example.com/test\n\ngo 1.25\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "dummy.go"),
					[]byte("package test\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(dir, ToolFileCanonical),
					[]byte("//go:build tools\n\npackage tools\n"),
					0o644,
				))
			},
			wantTool: ToolFileCanonical,
		},
		{
			name:       "rule file only",
			importPath: "example.com/test",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "go.mod"),
					[]byte("module example.com/test\n\ngo 1.25\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "dummy.go"),
					[]byte("package test\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "foo.otelc.yml"),
					[]byte("{}\n"),
					0o644,
				))
			},
			wantRules: []string{"foo.otelc.yml"},
		},
		{
			name:       "tool file and rule files",
			importPath: "example.com/test",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "go.mod"),
					[]byte("module example.com/test\n\ngo 1.25\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "dummy.go"),
					[]byte("package test\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(dir, ToolFileCanonical),
					[]byte("//go:build tools\n\npackage tools\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "foo.otelc.yml"),
					[]byte("{}\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "bar.otelc.yml"),
					[]byte("{}\n"),
					0o644,
				))
			},
			wantTool:  ToolFileCanonical,
			wantRules: []string{"foo.otelc.yml", "bar.otelc.yml"},
		},
		{
			name:       "both tool files",
			importPath: "example.com/test",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "go.mod"),
					[]byte("module example.com/test\n\ngo 1.25\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "dummy.go"),
					[]byte("package test\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(dir, ToolFileCanonical),
					[]byte("//go:build tools\n\npackage tools\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(dir, ToolFileAlias),
					[]byte("//go:build tools\n\npackage tools\n"),
					0o644,
				))
			},
			wantErr: true,
		},
		{
			name:       "no instrumentation config",
			importPath: "example.com/test",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "go.mod"),
					[]byte("module example.com/test\n\ngo 1.25\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "dummy.go"),
					[]byte("package test\n"),
					0o644,
				))
			},
			wantErr: true,
		},
		{
			name:       "rules from package",
			importPath: "example.com/test/sub",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "go.mod"),
					[]byte("module example.com/test\n\ngo 1.25\n"),
					0o644,
				))

				// Sub dir rules should be loaded.
				subDir := filepath.Join(dir, "sub")
				require.NoError(t, os.Mkdir(subDir, 0o755))

				require.NoError(t, os.WriteFile(
					filepath.Join(subDir, "dummy.go"),
					[]byte("package test\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(subDir, "foo.otelc.yml"),
					[]byte("{}\n"),
					0o644,
				))

				// Sub dir 2 rules should not be loaded.
				subDir2 := filepath.Join(dir, "sub2")
				require.NoError(t, os.Mkdir(subDir2, 0o755))

				require.NoError(t, os.WriteFile(
					filepath.Join(subDir2, "dummy.go"),
					[]byte("package test\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(subDir2, "bar.otelc.yml"),
					[]byte("{}\n"),
					0o644,
				))
			},
			wantRules: []string{"foo.otelc.yml"},
		},
		{
			name:       "does not load rules from submodules",
			importPath: "example.com/test",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "go.mod"),
					[]byte("module example.com/test\n\ngo 1.25\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "dummy.go"),
					[]byte("package test\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "foo.otelc.yml"),
					[]byte("{}\n"),
					0o644,
				))

				subDir := filepath.Join(dir, "sub")
				require.NoError(t, os.Mkdir(subDir, 0o755))

				require.NoError(t, os.WriteFile(
					filepath.Join(subDir, "go.mod"),
					[]byte("module example.com/test/sub\n\ngo 1.25\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(subDir, "bar.otelc.yml"),
					[]byte("{}\n"),
					0o644,
				))
			},
			wantRules: []string{"foo.otelc.yml" /* not bar.otelc.yml */},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(t, dir)

			cfg, err := resolveInstrumentationConfig(t.Context(), dir, tt.importPath)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.Equal(t, tt.importPath, cfg.ImportPath)

			require.NoError(t, err)
			require.NotNil(t, cfg)

			if tt.wantTool == "" {
				require.Empty(t, cfg.ToolFile)
			} else {
				require.Equal(t, filepath.Join(dir, tt.wantTool), cfg.ToolFile)
			}

			gotRules := make([]string, 0, len(cfg.RuleFiles))
			for _, f := range cfg.RuleFiles {
				gotRules = append(gotRules, filepath.Base(f))
			}
			require.ElementsMatch(t, tt.wantRules, gotRules)
		})
	}
}

func writeToolFile(t *testing.T, path string, imports ...string) {
	t.Helper()

	var b strings.Builder
	b.WriteString("//go:build tools\n\n")
	b.WriteString("package tools\n\n")
	b.WriteString("import (\n")
	for _, imp := range imports {
		fmt.Fprintf(&b, "\t_ %q\n", imp)
	}
	b.WriteString(")\n")

	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
}

func writeInstrumentationModule(
	t *testing.T,
	root, module string,
	writeDummyRules bool,
	imports map[string]string,
) string {
	t.Helper()

	require.NoError(t, os.MkdirAll(root, 0o755))

	goMod := fmt.Appendf(nil, "module %s\n\ngo 1.25\n", module)
	for imp := range imports {
		goMod = fmt.Appendf(goMod, "\nrequire %s v0.0.0-00010101000000-000000000000", imp)
	}
	for imp, replace := range imports {
		goMod = fmt.Appendf(goMod, "\nreplace %s => %s\n", imp, replace)
	}
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "go.mod"),
		goMod,
		0o644,
	))

	require.NoError(t, os.WriteFile(
		filepath.Join(root, "dummy.go"),
		[]byte("package dummy\n"),
		0o644,
	))

	if writeDummyRules {
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "dummy.otelc.yml"),
			[]byte(`dummyrule:
  target: main
  where:
    func: Example
  do:
    - inject_code:
        raw: "_ = 1"
`),
			0o644,
		))
	}

	if len(imports) > 0 {
		writeToolFile(t, filepath.Join(root, ToolFileCanonical), slices.Collect(maps.Keys(imports))...)
	}

	return filepath.Join(root, ToolFileCanonical)
}

func TestWalkInstrumentation_VisitsImports(t *testing.T) {
	tmp := t.TempDir()

	toolFile := writeInstrumentationModule(t, tmp, "example.com/root", false, map[string]string{
		util.OtelcToolCmdRoot: filepath.Join(tmp, "otelc"), // ignored by walkInstrumentation
		"example.com/foo":     filepath.Join(tmp, "foo"),
		"example.com/bar":     filepath.Join(tmp, "bar"),
	})
	writeInstrumentationModule(t, filepath.Join(tmp, "foo"), "example.com/foo", true, nil)
	writeInstrumentationModule(t, filepath.Join(tmp, "bar"), "example.com/bar", true, nil)

	var visits []string
	err := walkInstrumentation(t.Context(), []string{toolFile},
		func(v *InstrumentationVisit) (bool, error) {
			require.NoError(t, v.Error)

			visits = append(visits, v.Config.ImportPath)
			return true, nil
		},
	)

	require.NoError(t, err)
	require.ElementsMatch(t,
		[]string{
			"example.com/foo",
			"example.com/bar",
		},
		visits,
	)
}

func TestWalkInstrumentation_IgnoresNamedImports(t *testing.T) {
	tmp := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(tmp, "go.mod"),
		fmt.Appendf(nil, `module example.com/root

go 1.25

require (
	example.com/foo v0.0.0-00010101000000-000000000000
	example.com/bar v0.0.0-00010101000000-000000000000
)

replace example.com/foo => %s
replace example.com/bar => %s
`,
			filepath.Join(tmp, "foo"),
			filepath.Join(tmp, "bar"),
		),
		0o644,
	))

	require.NoError(t, os.WriteFile(
		filepath.Join(tmp, ToolFileCanonical),
		[]byte(`//go:build tools

package tools

import (
	_ "example.com/foo"
	bar "example.com/bar"
)
`),
		0o644,
	))

	writeInstrumentationModule(t, filepath.Join(tmp, "foo"), "example.com/foo", true, nil)
	writeInstrumentationModule(t, filepath.Join(tmp, "bar"), "example.com/bar", true, nil)

	var visits []string
	err := walkInstrumentation(t.Context(), []string{filepath.Join(tmp, ToolFileCanonical)},
		func(v *InstrumentationVisit) (bool, error) {
			require.NoError(t, v.Error)
			visits = append(visits, v.Config.ImportPath)
			return true, nil
		},
	)

	require.NoError(t, err)
	require.ElementsMatch(t, []string{"example.com/foo"}, visits)
}

func TestWalkInstrumentation_Recurses(t *testing.T) {
	tmp := t.TempDir()

	toolFile := filepath.Join(tmp, ToolFileCanonical)
	writeToolFile(t, toolFile, "example.com/foo")
	writeInstrumentationModule(t, tmp, "example.com/root", false, map[string]string{
		"example.com/foo": filepath.Join(tmp, "foo"),
	})
	writeInstrumentationModule(t, filepath.Join(tmp, "foo"), "example.com/foo", false, map[string]string{
		"example.com/bar": filepath.Join(tmp, "bar"),
	})
	writeInstrumentationModule(t, filepath.Join(tmp, "bar"), "example.com/bar", true, nil)

	var visits []string
	err := walkInstrumentation(t.Context(), []string{toolFile},
		func(v *InstrumentationVisit) (bool, error) {
			require.NoError(t, v.Error)

			visits = append(visits, v.Config.ImportPath)
			return true, nil
		},
	)

	require.NoError(t, err)
	require.ElementsMatch(t,
		[]string{
			"example.com/foo",
			"example.com/bar",
		},
		visits,
	)
}

func TestWalkInstrumentation_NoRecurse(t *testing.T) {
	tmp := t.TempDir()

	toolFile := writeInstrumentationModule(t, tmp, "example.com/root", false, map[string]string{
		"example.com/foo": filepath.Join(tmp, "foo"),
	})
	writeInstrumentationModule(t, filepath.Join(tmp, "foo"), "example.com/foo", false, map[string]string{
		"example.com/bar": filepath.Join(tmp, "bar"),
	})
	writeInstrumentationModule(t, filepath.Join(tmp, "bar"), "example.com/bar", true, nil)

	var visits []string
	err := walkInstrumentation(t.Context(), []string{toolFile},
		func(v *InstrumentationVisit) (bool, error) {
			require.NoError(t, v.Error)

			visits = append(visits, v.Config.ImportPath)
			return false, nil
		},
	)

	require.NoError(t, err)
	require.ElementsMatch(t,
		[]string{
			"example.com/foo",
		},
		visits,
	)
}

func TestWalkInstrumentation_DeduplicatesImports(t *testing.T) {
	tmp := t.TempDir()

	toolFile := writeInstrumentationModule(t, tmp, "example.com/root", false, map[string]string{
		"example.com/foo": filepath.Join(tmp, "foo"),
		"example.com/bar": filepath.Join(tmp, "bar"),
	})
	writeInstrumentationModule(t, filepath.Join(tmp, "foo"), "example.com/foo", true, map[string]string{
		"example.com/bar": filepath.Join(tmp, "bar"),
	})
	writeInstrumentationModule(t, filepath.Join(tmp, "bar"), "example.com/bar", true, nil)

	counts := make(map[string]int)
	err := walkInstrumentation(t.Context(), []string{toolFile},
		func(v *InstrumentationVisit) (bool, error) {
			require.NoError(t, v.Error)

			counts[v.Config.ImportPath]++
			return true, nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, 1, counts["example.com/foo"])
	require.Equal(t, 1, counts["example.com/bar"])
}

func TestWalkInstrumentation_AvoidsCycles(t *testing.T) {
	tmp := t.TempDir()

	toolFile := writeInstrumentationModule(t, tmp, "example.com/root", false, map[string]string{
		"example.com/foo": filepath.Join(tmp, "foo"),
		"example.com/bar": filepath.Join(tmp, "bar"),
	})
	writeInstrumentationModule(t, filepath.Join(tmp, "foo"), "example.com/foo", false, map[string]string{
		"example.com/bar": filepath.Join(tmp, "bar"),
	})
	writeInstrumentationModule(t, filepath.Join(tmp, "bar"), "example.com/bar", false, map[string]string{
		"example.com/foo": filepath.Join(tmp, "foo"),
	})

	var visits []string
	err := walkInstrumentation(t.Context(), []string{toolFile},
		func(v *InstrumentationVisit) (bool, error) {
			require.NoError(t, v.Error)

			visits = append(visits, v.Config.ImportPath)
			return true, nil
		},
	)

	require.NoError(t, err)
	require.ElementsMatch(t,
		[]string{
			"example.com/foo",
			"example.com/bar",
		},
		visits,
	)
}

func TestWalkInstrumentation_VisitError(t *testing.T) {
	tmp := t.TempDir()

	toolFile := writeInstrumentationModule(t, tmp, "example.com/root", false, map[string]string{
		"example.com/foo": filepath.Join(tmp, "foo"),
	})
	writeInstrumentationModule(t, filepath.Join(tmp, "foo"), "example.com/foo", true, nil)

	wantErr := errors.New("visit error")

	err := walkInstrumentation(t.Context(), []string{toolFile},
		func(v *InstrumentationVisit) (bool, error) {
			require.NoError(t, v.Error)

			return false, wantErr
		},
	)

	require.ErrorIs(t, err, wantErr)
}

func TestWalkInstrumentation_ResolveError(t *testing.T) {
	tmp := t.TempDir()

	toolFile := writeInstrumentationModule(t, tmp, "example.com/root", false, map[string]string{
		"example.com/notinstrumentation": filepath.Join(tmp, "notinstrumentation"),
	})
	// This module does not have a tool file or rule files, so it should return ErrNotInstrumentation.
	writeInstrumentationModule(
		t,
		filepath.Join(tmp, "notinstrumentation"),
		"example.com/notinstrumentation",
		false,
		nil,
	)

	var got *InstrumentationVisit
	err := walkInstrumentation(t.Context(), []string{toolFile},
		func(v *InstrumentationVisit) (bool, error) {
			got = v
			return false, nil
		},
	)

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Nil(t, got.Config)
	require.ErrorIs(t, got.Error, ErrNotInstrumentation)
}
