// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVendoringActive(t *testing.T) {
	writeVendoredModule := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "vendor"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "vendor", "modules.txt"), []byte(""), 0o644))
		return root
	}

	t.Run("vendor present at module root", func(t *testing.T) {
		assert.True(t, vendoringActive(t.Context(), writeVendoredModule(t)))
	})
	t.Run("vendor found from subdirectory", func(t *testing.T) {
		root := writeVendoredModule(t)
		sub := filepath.Join(root, "cmd")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		assert.True(t, vendoringActive(t.Context(), sub))
	})
	t.Run("module without vendor", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
		assert.False(t, vendoringActive(t.Context(), root))
	})
	t.Run("no module", func(t *testing.T) {
		assert.False(t, vendoringActive(t.Context(), t.TempDir()))
	})
	t.Run("vendor present but in a workspace", func(t *testing.T) {
		root := writeVendoredModule(t)
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "go.work"),
			[]byte("go 1.25.0\n\nuse .\n"),
			0o644,
		))
		// -mod=mod is forbidden in workspace mode, so a leftover vendor/modules.txt
		// must not make this report true.
		assert.False(t, vendoringActive(t.Context(), root))
	})
}

func TestForceModMod(t *testing.T) {
	tests := []struct {
		name    string
		goflags string
		want    string
	}{
		{"empty appends", "", "-mod=mod"},
		{"no mod token appends", "-trimpath", "-trimpath -mod=mod"},
		{"vendor overridden", "-mod=vendor", "-mod=mod"},
		{"vendor overridden among flags", "-trimpath -mod=vendor", "-trimpath -mod=mod"},
		{"mod left unchanged", "-mod=mod", "-mod=mod"},
		{"readonly left unchanged", "-mod=readonly", "-mod=readonly"},
		{"readonly among flags left unchanged", "-trimpath -mod=readonly", "-trimpath -mod=readonly"},
		// Go applies last-wins for a repeated flag, so every -mod=vendor must be
		// rewritten and no -mod=mod appended when a -mod token already exists.
		{"readonly then vendor last wins", "-mod=readonly -mod=vendor", "-mod=readonly -mod=mod"},
		{"duplicate vendor both rewritten", "-mod=vendor -mod=vendor", "-mod=mod -mod=mod"},
		{"bare mod left as is, no append", "-mod", "-mod"},
		// A -mod substring in another flag is not a -mod token, so -mod=mod is appended.
		{"modcacherw is not a mod flag", "-modcacherw=true", "-modcacherw=true -mod=mod"},
		{"mod substring in another value", "-ldflags=-X=v=-mod=x", "-ldflags=-X=v=-mod=x -mod=mod"},
		// Go's flag parser treats the double-dash form the same as single-dash.
		{"double-dash vendor overridden", "--mod=vendor", "-mod=mod"},
		{"double-dash mod left unchanged, no append", "--mod=mod", "--mod=mod"},
		{"double-dash readonly left unchanged, no append", "--mod=readonly", "--mod=readonly"},
		{"bare double-dash mod left as is, no append", "--mod", "--mod"},
		// The go command accepts quoted GOFLAGS tokens. A quoted -mod flag is
		// still a -mod flag, so it must be recognized and not have -mod=mod
		// appended after it, which would win by last-value.
		{"single-quoted readonly left unchanged", "'-mod=readonly'", "'-mod=readonly'"},
		{"double-quoted readonly left unchanged", `"-mod=readonly"`, `"-mod=readonly"`},
		{"quoted readonly among flags left unchanged", "-trimpath '-mod=readonly'", "-trimpath '-mod=readonly'"},
		{"quoted vendor overridden", "'-mod=vendor'", "-mod=mod"},
		{"quoted double-dash readonly left unchanged", "'--mod=readonly'", "'--mod=readonly'"},
		{"quoted double-dash vendor overridden", "'--mod=vendor'", "-mod=mod"},
		// A quoted flag whose value contains a space stays one token, so the
		// following -mod flag is still recognized and not clobbered.
		{"spaced quoted flag before quoted mod", "-tags='foo bar' '-mod=readonly'", "-tags='foo bar' '-mod=readonly'"},
		{"spaced quoted flag no mod appends once", "-tags='foo bar'", "-tags='foo bar' -mod=mod"},
		// The go command strips only a surrounding quote pair, so -mod='vendor' keeps
		// its quoted value and is not a -mod=vendor token. It stays as written and go
		// rejects the value later.
		{"value-quoted vendor preserved; go rejects it later", "-mod='vendor'", "-mod='vendor'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, forceModMod(tt.goflags))
		})
	}
}

func TestRewriteModVendor(t *testing.T) {
	tests := []struct {
		name       string
		subcommand string
		args       []string
		want       []string
	}{
		{
			name:       "vendor single token",
			subcommand: subcmdBuild,
			args:       []string{"build", "-mod=vendor", "./..."},
			want:       []string{"build", "-mod=mod", "./..."},
		},
		{
			name:       "vendor two token",
			subcommand: subcmdBuild,
			args:       []string{"build", "-mod", "vendor", "./..."},
			want:       []string{"build", "-mod", "mod", "./..."},
		},
		{
			name:       "readonly untouched",
			subcommand: subcmdBuild,
			args:       []string{"build", "-mod=readonly", "./..."},
			want:       []string{"build", "-mod=readonly", "./..."},
		},
		{
			name:       "mod untouched",
			subcommand: subcmdBuild,
			args:       []string{"build", "-mod=mod", "./..."},
			want:       []string{"build", "-mod=mod", "./..."},
		},
		{
			name:       "no mod flag",
			subcommand: subcmdBuild,
			args:       []string{"build", "-race", "./..."},
			want:       []string{"build", "-race", "./..."},
		},
		// -mod as the last arg: the two-token branch must not index past the end.
		{
			name:       "bare mod as last arg",
			subcommand: subcmdBuild,
			args:       []string{"build", "-mod"},
			want:       []string{"build", "-mod"},
		},
		{
			name:       "multiple occurrences all rewritten",
			subcommand: subcmdBuild,
			args:       []string{"build", "-mod=vendor", "-o", "x", "-mod", "vendor"},
			want:       []string{"build", "-mod=mod", "-o", "x", "-mod", "mod"},
		},
		// A positional "vendor" not preceded by -mod is a build target, not a flag.
		{
			name:       "positional vendor left alone",
			subcommand: subcmdBuild,
			args:       []string{"build", "vendor"},
			want:       []string{"build", "vendor"},
		},
		// Go's flag parser treats the double-dash form the same as single-dash.
		{
			name:       "double-dash vendor single token",
			subcommand: subcmdBuild,
			args:       []string{"build", "--mod=vendor", "./..."},
			want:       []string{"build", "-mod=mod", "./..."},
		},
		{
			name:       "double-dash vendor two token",
			subcommand: subcmdBuild,
			args:       []string{"build", "--mod", "vendor", "./..."},
			want:       []string{"build", "--mod", "mod", "./..."},
		},
		{
			name:       "double-dash readonly untouched",
			subcommand: subcmdBuild,
			args:       []string{"build", "--mod=readonly", "./..."},
			want:       []string{"build", "--mod=readonly", "./..."},
		},
		// Test-binary arguments after delimiters must never be mutated
		{
			name:       "test delimiter dash-dash preserves -mod=vendor tail",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "--", "-mod=vendor"},
			want:       []string{"./pkg", "--", "-mod=vendor"},
		},
		{
			name:       "test delimiter -args preserves -mod=vendor tail",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "-args", "-mod=vendor"},
			want:       []string{"./pkg", "-args", "-mod=vendor"},
		},
		{
			name:       "test delimiter --args preserves -mod=vendor tail",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "--args", "-mod=vendor"},
			want:       []string{"./pkg", "--args", "-mod=vendor"},
		},
		{
			name:       "test delimiter dash-dash preserves -mod vendor two-token tail",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "--", "-mod", "vendor"},
			want:       []string{"./pkg", "--", "-mod", "vendor"},
		},
		// Test flag values that resemble build flags must never be mutated
		{
			name:       "test flag value -test.run preserves -mod=vendor",
			subcommand: subcmdTest,
			args:       []string{"-test.run", "-mod=vendor", "./pkg"},
			want:       []string{"-test.run", "-mod=vendor", "./pkg"},
		},
		{
			name:       "test flag value -run preserves -mod=vendor",
			subcommand: subcmdTest,
			args:       []string{"-run", "-mod=vendor", "./pkg"},
			want:       []string{"-run", "-mod=vendor", "./pkg"},
		},
		// Real build flags in go test must be rewritten
		{
			name:       "genuine build flag in go test is rewritten",
			subcommand: subcmdTest,
			args:       []string{"-mod=vendor", "./pkg"},
			want:       []string{"-mod=mod", "./pkg"},
		},
		{
			name:       "genuine two-token build flag in go test is rewritten",
			subcommand: subcmdTest,
			args:       []string{"-mod", "vendor", "./pkg"},
			want:       []string{"-mod", "mod", "./pkg"},
		},
		// Unknown test flag value is preserved
		{
			name:       "unknown test flag value preserves -mod=vendor",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "-custom=-mod=vendor"},
			want:       []string{"./pkg", "-custom=-mod=vendor"},
		},
		// Positional test-argv boundary preserves -mod=vendor
		{
			name:       "positional test arg preserves -mod=vendor",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "-run", "TestX", "positional", "-mod=vendor"},
			want:       []string{"./pkg", "-run", "TestX", "positional", "-mod=vendor"},
		},
		{
			name:       "joined unknown flag followed by positional preserves -mod=vendor",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "-custom=x", "positional", "-mod=vendor"},
			want:       []string{"./pkg", "-custom=x", "positional", "-mod=vendor"},
		},
		{
			name:       "definitive positional tail preserves -mod=vendor",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "-run", "TestX", "positional", "-race", "-mod=vendor", "-tags=x", "./other"},
			want:       []string{"./pkg", "-run", "TestX", "positional", "-race", "-mod=vendor", "-tags=x", "./other"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subcmd := tt.subcommand
			if subcmd == "" {
				subcmd = subcmdBuild
			}
			assert.Equal(t, tt.want, rewriteModVendor(subcmd, tt.args))
		})
	}
}
