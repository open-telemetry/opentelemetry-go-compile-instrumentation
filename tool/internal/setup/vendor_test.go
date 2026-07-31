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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, forceModMod(tt.goflags))
		})
	}
}

func TestRewriteModVendor(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"vendor single token", []string{"build", "-mod=vendor", "./..."}, []string{"build", "-mod=mod", "./..."}},
		{"vendor two token", []string{"build", "-mod", "vendor", "./..."}, []string{"build", "-mod", "mod", "./..."}},
		{
			"readonly untouched",
			[]string{"build", "-mod=readonly", "./..."},
			[]string{"build", "-mod=readonly", "./..."},
		},
		{"mod untouched", []string{"build", "-mod=mod", "./..."}, []string{"build", "-mod=mod", "./..."}},
		{"no mod flag", []string{"build", "-race", "./..."}, []string{"build", "-race", "./..."}},
		// -mod as the last arg: the two-token branch must not index past the end.
		{"bare mod as last arg", []string{"build", "-mod"}, []string{"build", "-mod"}},
		{
			"multiple occurrences all rewritten",
			[]string{"build", "-mod=vendor", "-o", "x", "-mod", "vendor"},
			[]string{"build", "-mod=mod", "-o", "x", "-mod", "mod"},
		},
		// A positional "vendor" not preceded by -mod is a build target, not a flag.
		{"positional vendor left alone", []string{"build", "vendor"}, []string{"build", "vendor"}},
		// Go's flag parser treats the double-dash form the same as single-dash.
		{
			"double-dash vendor single token",
			[]string{"build", "--mod=vendor", "./..."},
			[]string{"build", "-mod=mod", "./..."},
		},
		{
			"double-dash vendor two token",
			[]string{"build", "--mod", "vendor", "./..."},
			[]string{"build", "--mod", "mod", "./..."},
		},
		{
			"double-dash readonly untouched",
			[]string{"build", "--mod=readonly", "./..."},
			[]string{"build", "--mod=readonly", "./..."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, rewriteModVendor(tt.args))
		})
	}
}
