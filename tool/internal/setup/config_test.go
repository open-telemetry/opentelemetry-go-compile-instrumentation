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
		wantErr error
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
			wantErr: ErrNotInstrumentation,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(dir)

			got, err := findToolFile(dir)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
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
		name      string
		setup     func(t *testing.T, dir string) string
		wantErr   error
		wantTool  string
		wantRules []string
	}{
		{
			name: "tool file only",
			setup: func(t *testing.T, dir string) string {
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

				return "example.com/test"
			},
			wantTool: ToolFileCanonical,
		},
		{
			name: "rule file only",
			setup: func(t *testing.T, dir string) string {
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

				return "example.com/test"
			},
			wantRules: []string{"foo.otelc.yml"},
		},
		{
			name: "tool file and rule files",
			setup: func(t *testing.T, dir string) string {
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

				return "example.com/test"
			},
			wantTool:  ToolFileCanonical,
			wantRules: []string{"foo.otelc.yml", "bar.otelc.yml"},
		},
		{
			name: "both tool files",
			setup: func(t *testing.T, dir string) string {
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

				return "example.com/test"
			},
			wantErr: ErrNotInstrumentation,
		},
		{
			name: "no instrumentation config",
			setup: func(t *testing.T, dir string) string {
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

				return "example.com/test"
			},
			wantErr: ErrNotInstrumentation,
		},
		{
			name: "not module root",
			setup: func(t *testing.T, dir string) string {
				require.NoError(t, os.WriteFile(
					filepath.Join(dir, "go.mod"),
					[]byte("module example.com/test\n\ngo 1.25\n"),
					0o644,
				))

				// Valid Go package, but not a module root.
				subDir := filepath.Join(dir, "sub")
				require.NoError(t, os.Mkdir(subDir, 0o755))

				require.NoError(t, os.WriteFile(
					filepath.Join(subDir, "dummy.go"),
					[]byte("package test\n"),
					0o644,
				))

				require.NoError(t, os.WriteFile(
					filepath.Join(subDir, ToolFileCanonical),
					[]byte("//go:build tools\n\npackage tools\n"),
					0o644,
				))

				return "example.com/test/sub"
			},
			wantErr: ErrNotInstrumentation,
		},
		{
			name: "does not load rules from submodules",
			setup: func(t *testing.T, dir string) string {
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

				return "example.com/test"
			},
			wantRules: []string{"foo.otelc.yml" /* not bar.otelc.yml */},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			importPath := tt.setup(t, dir)

			cfg, err := resolveInstrumentationConfig(t.Context(), dir, importPath)

			if tt.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cfg)

			require.Equal(t, importPath, cfg.ImportPath)
			require.Equal(t, dir, cfg.Dir)

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

			visits = append(visits, v.ImportPath)
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
			visits = append(visits, v.ImportPath)
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

			visits = append(visits, v.ImportPath)
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

			visits = append(visits, v.ImportPath)
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

			counts[v.ImportPath]++
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

			visits = append(visits, v.ImportPath)
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
	require.Equal(t, "example.com/notinstrumentation", got.ImportPath)
	require.Nil(t, got.Config)
	require.ErrorIs(t, got.Error, ErrNotInstrumentation)
}
