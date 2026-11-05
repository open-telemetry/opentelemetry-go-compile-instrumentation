// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/pkgload"
)

func TestNormalizeModuleDirs(t *testing.T) {
	t.Run("empty slice returns empty non-nil slice", func(t *testing.T) {
		got := normalizeModuleDirs(nil)
		assert.NotNil(t, got)
		assert.Empty(t, got)

		got = normalizeModuleDirs([]string{})
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})

	t.Run("sorts and compacts without mutating caller slice", func(t *testing.T) {
		dirs := []string{"/dir/c", "/dir/a", "/dir/b", "/dir/a", "/dir/c"}
		dirsOrig := append([]string(nil), dirs...)

		got := normalizeModuleDirs(dirs)
		assert.Equal(t, []string{"/dir/a", "/dir/b", "/dir/c"}, got)
		assert.Equal(t, dirsOrig, dirs, "caller slice must not be mutated")
	})
}

const testMainGo = "package main\n\nfunc main() {}\n"

func plainModuleFiles() map[string]string {
	return map[string]string{
		"go.mod":  "module example.com/app\n\ngo 1.25\n",
		"main.go": testMainGo,
	}
}

func changeDirFiles() map[string]string {
	return map[string]string{
		"app/go.mod":  "module example.com/app\n\ngo 1.25\n",
		"app/main.go": testMainGo,
	}
}

func workspaceFiles() map[string]string {
	return map[string]string{
		"go.work":   "go 1.25\n\nuse (\n\t./a\n\t./b\n)\n",
		"a/go.mod":  "module example.com/a\n\ngo 1.25\n",
		"a/main.go": testMainGo,
		"b/go.mod":  "module example.com/b\n\ngo 1.25\n",
		"b/main.go": testMainGo,
	}
}

// newBuildRoot writes files (slash-separated path -> content) into a temp dir
// and changes into it. Workspace mode is off unless files has a go.work, so a
// go.work around the temp dir cannot change the result.
func newBuildRoot(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		mustWriteFile(t, filepath.Join(root, filepath.FromSlash(rel)), content)
	}
	gowork := "off"
	if _, ok := files["go.work"]; ok {
		gowork = filepath.Join(root, "go.work")
	}
	t.Setenv("GOWORK", gowork)
	t.Setenv("GOFLAGS", "")
	t.Chdir(root)
	return root
}

// realDirs resolves symbolic links (and short names on Windows), so a path
// under t.TempDir compares equal to the module dir that go list reports.
func realDirs(t *testing.T, dirs []string) []string {
	t.Helper()
	res := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		resolved, err := filepath.EvalSymlinks(dir)
		require.NoError(t, err)
		res = append(res, resolved)
	}
	return res
}

// testOnlyFiles holds a package with only in-package tests and one with only
// external tests. Neither has GoFiles.
func testOnlyFiles(prefix string) map[string]string {
	return map[string]string{
		prefix + "go.mod":          "module example.com/app\n\ngo 1.25\n",
		prefix + "inpkg/a_test.go": "package inpkg\n",
		prefix + "xpkg/b_test.go":  "package xpkg_test\n",
	}
}

func TestFindBuildModuleDirs(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		subcommand string // defaults to build
		args       []string
		want       []string // module dirs, relative to the build root
	}{
		{
			name:  "plain module",
			files: plainModuleFiles(),
			args:  []string{"."},
			want:  []string{"."},
		},
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
		{
			name: "nested module is not part of ./...",
			files: map[string]string{
				"go.mod":        "module example.com/outer\n\ngo 1.25\n",
				"main.go":       testMainGo,
				"inner/go.mod":  "module example.com/inner\n\ngo 1.25\n",
				"inner/main.go": testMainGo,
			},
			args: []string{"./..."},
			want: []string{"."},
		},
		{
			name:       "package with only in-package tests",
			files:      testOnlyFiles(""),
			subcommand: subcmdTest,
			args:       []string{"./inpkg"},
			want:       []string{"."},
		},
		{
			name:       "package with only external tests",
			files:      testOnlyFiles(""),
			subcommand: subcmdTest,
			args:       []string{"./xpkg"},
			want:       []string{"."},
		},
		{
			name:       "module with only test-only packages",
			files:      testOnlyFiles(""),
			subcommand: subcmdTest,
			args:       []string{"./..."},
			want:       []string{"."},
		},
		{
			name: "test-only package next to a package of another module",
			files: func() map[string]string {
				files := testOnlyFiles("b/")
				files["go.work"] = "go 1.25\n\nuse (\n\t./a\n\t./b\n)\n"
				files["a/go.mod"] = "module example.com/a\n\ngo 1.25\n"
				files["a/main.go"] = testMainGo
				return files
			}(),
			subcommand: subcmdTest,
			args:       []string{"./a", "./b/inpkg"},
			want:       []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newBuildRoot(t, tt.files)
			subcommand := tt.subcommand
			if subcommand == "" {
				subcommand = subcmdBuild
			}

			got, err := findBuildModuleDirs(t.Context(), subcommand, tt.args)
			require.NoError(t, err)

			buildPkgs, _, err := getBuildPackages(t.Context(), subcommand, tt.args)
			require.NoError(t, err)
			full, err := pkgload.FindModuleDirs(t.Context(), buildPkgs)
			require.NoError(t, err)
			assert.Equal(t, full, got, "module dirs differ from the full load")

			want := make([]string, 0, len(tt.want))
			for _, rel := range tt.want {
				want = append(want, filepath.Join(root, rel))
			}
			assert.Equal(t, realDirs(t, want), realDirs(t, got), "unexpected module dirs")
		})
	}
}
