// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestGetPackages(t *testing.T) {
	setupTestModule(t, []string{"cmd", "foo/demo"})

	tests := []struct {
		name             string
		args             []string
		expectedCount    int
		expectedPackages []string
	}{
		{
			name:             "single package",
			args:             []string{"build", "-a", "-o", "tmp", "./cmd"},
			expectedCount:    1,
			expectedPackages: []string{"testmodule/cmd"},
		},
		{
			name:             "multiple packages",
			args:             []string{"build", "./cmd", "./foo/demo"},
			expectedCount:    2,
			expectedPackages: []string{"testmodule/cmd", "testmodule/foo/demo"},
		},
		{
			name:             "wildcard pattern",
			args:             []string{"build", "./cmd/..."},
			expectedCount:    1,
			expectedPackages: []string{"testmodule/cmd"},
		},
		{
			name:             "default to current directory",
			args:             []string{"build"},
			expectedCount:    1,
			expectedPackages: []string{"."},
		},
		{
			name:             "current directory explicit",
			args:             []string{"build", "."},
			expectedCount:    1,
			expectedPackages: []string{"."},
		},
		{
			name:             "nonexistent package mixed with valid",
			args:             []string{"build", "./cmd", "./nonexistent"},
			expectedCount:    1,
			expectedPackages: []string{"testmodule/cmd"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkgs, err := getBuildPackages(t.Context(), tt.args)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(pkgs) != tt.expectedCount {
				t.Errorf("Expected %d packages, got %d", tt.expectedCount, len(pkgs))
			}

			if tt.expectedPackages != nil {
				pkgIDs := extractPackageIDs(pkgs)
				checkPackages(t, pkgIDs, tt.expectedPackages)
			}
		})
	}
}

func extractPackageIDs(pkgs []*packages.Package) []string {
	ids := make([]string, len(pkgs))
	for i, pkg := range pkgs {
		ids[i] = pkg.ID
	}
	return ids
}

// checkPackages verifies all expected strings are found in the packages.
func checkPackages(t *testing.T, pkgs, expectedPkgs []string) {
	t.Helper()
	if len(pkgs) == 0 {
		t.Fatal("No packages to check")
	}

	for _, exp := range expectedPkgs {
		if !slices.ContainsFunc(pkgs, func(pkg string) bool { return strings.Contains(pkg, exp) }) {
			t.Errorf("Expected package containing %q not found in %v", exp, pkgs)
		}
	}
}

// setupTestModule creates a temporary Go module with the given subdirectories.
// Each subdirectory will contain a simple main.go file.
func setupTestModule(t *testing.T, subDirs []string) {
	t.Helper()

	tmpDir := t.TempDir()

	for _, dir := range subDirs {
		fullPath := filepath.Join(tmpDir, dir)
		if err := os.MkdirAll(fullPath, 0o755); err != nil {
			t.Fatalf("Failed to create dir %s: %v", fullPath, err)
		}

		goFile := filepath.Join(fullPath, "main.go")
		if err := os.WriteFile(goFile, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
			t.Fatalf("Failed to create Go file %s: %v", goFile, err)
		}
	}

	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module testmodule\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	t.Chdir(tmpDir)
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
			result := getPackageDir(pkg)
			if result != expected {
				t.Errorf("getPackageDir() = %q, expected %q", result, expected)
			}
		})
	}
}
