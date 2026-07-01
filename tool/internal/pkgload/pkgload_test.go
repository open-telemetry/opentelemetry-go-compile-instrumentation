// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pkgload

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

func TestLoadPackages(t *testing.T) {
	pkgs, err := LoadPackages(t.Context(), packages.NeedName, nil, "fmt")
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.Equal(t, "fmt", pkgs[0].Name)
	assert.Equal(t, "fmt", pkgs[0].PkgPath)
}

func TestResolvePackageName(t *testing.T) {
	tests := []struct {
		importPath string
		expected   string
	}{
		{"fmt", "fmt"},
		{"encoding/json", "json"},
		{"net/http", "http"},
		{"context", "context"},
		{"io", "io"},
		{"strings", "strings"},
		{"sync", "sync"},
		{"time", "time"},
		{"github.com/dave/dst", "dst"},
		{"github.com/stretchr/testify/assert", "assert"},
	}

	for _, tt := range tests {
		t.Run(tt.importPath, func(t *testing.T) {
			result := ResolvePackageName(t.Context(), tt.importPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveExportFiles(t *testing.T) {
	ctx := t.Context()

	// Test with a standard library package
	archives, err := ResolveExportFiles(ctx, "fmt")
	require.NoError(t, err)

	// Should have fmt and its dependencies
	fmtArchive, exists := archives["fmt"]
	assert.True(t, exists, "fmt should be in the result")
	assert.NotEmpty(t, fmtArchive, "fmt archive path should not be empty")

	// fmt depends on other packages, so we should have more than one
	assert.Greater(t, len(archives), 1, "should have dependencies")

	t.Logf("Resolved %d packages for fmt", len(archives))
	t.Logf("fmt archive: %s", fmtArchive)
}

func TestResolveExportFiles_InvalidPackage(t *testing.T) {
	ctx := t.Context()

	// Test with a non-existent package
	_, err := ResolveExportFiles(ctx, "this/package/does/not/exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading package")
}

func TestResolveExportFiles_MultiplePackages(t *testing.T) {
	ctx := t.Context()

	// Test with net/http which has many dependencies
	archives, err := ResolveExportFiles(ctx, "net/http")
	require.NoError(t, err)

	// Should include net/http itself
	httpArchive, exists := archives["net/http"]
	assert.True(t, exists, "net/http should be in the result")
	assert.NotEmpty(t, httpArchive, "net/http archive path should not be empty")
	assert.FileExists(t, httpArchive, "net/http export file should exist")

	// Should include some of its dependencies
	assert.Contains(t, archives, "net")
	assert.Contains(t, archives, "fmt")
	assert.FileExists(t, archives["net"], "net export file should exist")
	assert.FileExists(t, archives["fmt"], "fmt export file should exist")

	t.Logf("Resolved %d packages for net/http", len(archives))
}

func TestResolveExportFiles_NoExportFile(t *testing.T) {
	ctx := t.Context()

	// Test with "unsafe" which has no export archive
	archives, err := ResolveExportFiles(ctx, "unsafe")
	require.Error(t, err, "unsafe package should not have an export archive")
	assert.Contains(t, err.Error(), "not found or has no export file")
	assert.Nil(t, archives)
}

func TestGetPackageDir(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		goFiles []string
	}{
		{
			name:    "package with single go file",
			goFiles: []string{filepath.Join("path_to_project", "main.go")},
		},
		{
			name:    "package with multiple go files",
			goFiles: []string{filepath.Join("path_to_project", "main.go"), filepath.Join("path_to_project", "util.go")},
		},
		{
			name:    "package with nested path",
			goFiles: []string{filepath.Join("path_to_project", "cmd", "server", "main.go")},
		},
		{
			name:    "package with absolute path",
			goFiles: []string{filepath.Join(tmpDir, "main.go")},
		},
		{
			name:    "package with no go files",
			goFiles: nil,
		},
		{
			name:    "package with empty go files slice",
			goFiles: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var expected string
			if len(tt.goFiles) > 0 {
				expected = filepath.Dir(tt.goFiles[0])
			}

			pkg := &packages.Package{}
			pkg.GoFiles = tt.goFiles
			result := GetPackageDir(pkg)
			if result != expected {
				t.Errorf("GetPackageDir() = %q, expected %q", result, expected)
			}
		})
	}
}

func TestResolveModuleDir(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T, root string) string
		expectedDir string
		expectError bool
	}{
		{
			name: "finds go.mod in current directory",
			setup: func(t *testing.T, root string) string {
				err := os.WriteFile(
					filepath.Join(root, "go.mod"),
					[]byte("module example.com/test\n"),
					0o644,
				)
				require.NoError(t, err)

				err = os.WriteFile(
					filepath.Join(root, "main.go"),
					[]byte("package main\n\nfunc main() {}\n"),
					0o644,
				)
				require.NoError(t, err)

				return root
			},
			expectedDir: ".",
		},
		{
			name: "finds go.mod in parent directory",
			setup: func(t *testing.T, root string) string {
				err := os.WriteFile(
					filepath.Join(root, "go.mod"),
					[]byte("module example.com/test\n"),
					0o644,
				)
				require.NoError(t, err)

				nested := filepath.Join(root, "a", "b", "c")
				err = os.MkdirAll(nested, 0o755)
				require.NoError(t, err)

				err = os.WriteFile(
					filepath.Join(nested, "main.go"),
					[]byte("package main\n\nfunc main() {}\n"),
					0o644,
				)
				require.NoError(t, err)

				return nested
			},
			expectedDir: ".",
		},
		{
			name: "returns error when no go.mod exists",
			setup: func(t *testing.T, root string) string {
				return root
			},
			expectError: true,
		},
		{
			name: "fails for directory without go files",
			setup: func(t *testing.T, root string) string {
				err := os.WriteFile(
					filepath.Join(root, "go.mod"),
					[]byte("module example.com/test\n"),
					0o644,
				)
				require.NoError(t, err)

				emptyDir := filepath.Join(root, "empty")
				err = os.MkdirAll(emptyDir, 0o755)
				require.NoError(t, err)

				return emptyDir
			},
			expectError: true,
		},
		{
			name: "fails for build-tag-excluded package",
			setup: func(t *testing.T, root string) string {
				err := os.WriteFile(
					filepath.Join(root, "go.mod"),
					[]byte("module example.com/test\n"),
					0o644,
				)
				require.NoError(t, err)

				err = os.WriteFile(
					filepath.Join(root, "main.go"),
					[]byte("//go:build never\n\npackage main\n"),
					0o644,
				)
				require.NoError(t, err)

				return root
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			workDir := tt.setup(t, tmpDir)

			t.Chdir(workDir)

			ctx := t.Context()
			moduleDir, err := resolveModuleDir(ctx, workDir)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			expectedDir := tmpDir
			if tt.expectedDir != "." {
				expectedDir = tt.expectedDir
			}

			require.Equal(t, expectedDir, moduleDir)
		})
	}
}

func TestFindModuleDirs(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	require.NoError(t, os.WriteFile(
		filepath.Join(tmp, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"),
		0o644,
	))

	mainFile := filepath.Join(tmp, "main.go")
	require.NoError(t, os.WriteFile(
		mainFile,
		[]byte("package main\n"),
		0o644,
	))

	tests := []struct {
		name    string
		pkgs    []*packages.Package
		want    map[string]bool
		wantErr bool
	}{
		{
			name: "collects module dirs",
			pkgs: []*packages.Package{
				{
					PkgPath: "example.com/a",
					Module: &packages.Module{
						Dir: "/tmp/moda",
					},
				},
				{
					PkgPath: "example.com/b",
					Module: &packages.Module{
						Dir: "/tmp/modb",
					},
				},
			},
			want: map[string]bool{
				"/tmp/moda": true,
				"/tmp/modb": true,
			},
		},
		{
			name: "deduplicates module dirs",
			pkgs: []*packages.Package{
				{
					PkgPath: "example.com/a",
					Module: &packages.Module{
						Dir: "/tmp/mod",
					},
				},
				{
					PkgPath: "example.com/b",
					Module: &packages.Module{
						Dir: "/tmp/mod",
					},
				},
			},
			want: map[string]bool{
				"/tmp/mod": true,
			},
		},
		{
			name: "skips package without module",
			pkgs: []*packages.Package{
				{
					PkgPath: "example.com/a",
				},
			},
			want: map[string]bool{},
		},
		{
			name: "skips command line package without go files",
			pkgs: []*packages.Package{
				{
					PkgPath: CommandLineArgumentsPackage,
				},
			},
			want: map[string]bool{},
		},
		{
			name: "resolves module dir for command line package",
			pkgs: []*packages.Package{
				{
					PkgPath: CommandLineArgumentsPackage,
					GoFiles: []string{mainFile},
				},
			},
			want: map[string]bool{
				tmp: true,
			},
		},
		{
			name: "errors when command line package is not inside a module",
			pkgs: []*packages.Package{
				{
					PkgPath: CommandLineArgumentsPackage,
					GoFiles: []string{
						filepath.Join(t.TempDir(), "main.go"),
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FindModuleDirs(t.Context(), tt.pkgs)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
