// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
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

func TestSetupGoCache(t *testing.T) {
	t.Run("respects existing GOCACHE", func(t *testing.T) {
		t.Setenv("GOCACHE", "/existing/cache")
		env, err := setupGoCache(t.Context(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, e := range env {
			if strings.HasPrefix(e, "GOCACHE=") {
				t.Error("should not add GOCACHE when already set")
			}
		}
	})

	t.Run("creates persistent cache in .otel-build/gocache", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelWorkDir, tempDir)
		if err := os.MkdirAll(util.GetBuildTempDir(), 0o755); err != nil {
			t.Fatal(err)
		}

		env, err := setupGoCache(t.Context(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var cacheDir string
		for _, e := range env {
			if strings.HasPrefix(e, "GOCACHE=") {
				cacheDir = strings.TrimPrefix(e, "GOCACHE=")
				break
			}
		}
		if cacheDir == "" {
			t.Fatal("GOCACHE not set in environment")
		}
		expectedCacheDir := util.GetBuildTemp("gocache")
		if cacheDir != expectedCacheDir {
			t.Errorf("expected cache directory %s, got %s", expectedCacheDir, cacheDir)
		}
		if _, statErr := os.Stat(cacheDir); os.IsNotExist(statErr) {
			t.Errorf("cache directory not created: %s", cacheDir)
		}
	})
}

func TestExtractBuildFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "no build flags",
			args:     []string{"build", "-o", "output", "./..."},
			expected: nil,
		},
		{
			name:     "tags with equals",
			args:     []string{"build", "-tags=integration,e2e", "./..."},
			expected: []string{"-tags=integration,e2e"},
		},
		{
			name:     "tags with space separator",
			args:     []string{"build", "-tags", "integration,e2e", "./..."},
			expected: []string{"-tags", "integration,e2e"},
		},
		{
			name:     "tags with spaces in value",
			args:     []string{"build", "-tags", "foo bar", "./..."},
			expected: []string{"-tags", "foo bar"},
		},
		{
			name:     "race flag",
			args:     []string{"build", "-race", "./..."},
			expected: []string{"-race"},
		},
		{
			name:     "mod flag",
			args:     []string{"build", "-mod=vendor", "./..."},
			expected: []string{"-mod=vendor"},
		},
		{
			name:     "multiple flags",
			args:     []string{"build", "-tags=foo", "-race", "-mod=vendor", "./..."},
			expected: []string{"-tags=foo", "-mod=vendor", "-race"}, // value flags first, then sorted bool flags
		},
		{
			name:     "mixed format",
			args:     []string{"build", "-tags", "foo", "-mod=readonly", "-cover", "./..."},
			expected: []string{"-tags", "foo", "-mod=readonly", "-cover"}, // value flags first, then sorted bool flags
		},
		{
			name:     "ignores non-context flags",
			args:     []string{"build", "-v", "-x", "-tags=foo", "-o", "output", "./..."},
			expected: []string{"-tags=foo"},
		},
		{
			name:     "modfile flag",
			args:     []string{"build", "-modfile=go.custom.mod", "./..."},
			expected: []string{"-modfile=go.custom.mod"},
		},
		{
			name:     "modfile with spaces in path",
			args:     []string{"build", "-modfile", "path with spaces/go.mod", "./..."},
			expected: []string{"-modfile", "path with spaces/go.mod"},
		},
		{
			name:     "race=true is normalized",
			args:     []string{"build", "-race=true", "./..."},
			expected: []string{"-race"},
		},
		{
			name:     "race=false is excluded",
			args:     []string{"build", "-race=false", "./..."},
			expected: []string{"-race=false"},
		},
		{
			name:     "cover=true is normalized",
			args:     []string{"build", "-cover=true", "./..."},
			expected: []string{"-cover"},
		},
		{
			name:     "mixed bool formats",
			args:     []string{"build", "-race=true", "-cover", "-msan=false", "./..."},
			expected: []string{"-cover", "-msan=false", "-race"}, // sorted alphabetically
		},
		{
			name:     "race=1 is truthy",
			args:     []string{"build", "-race=1", "./..."},
			expected: []string{"-race"},
		},
		{
			name:     "race=T is truthy",
			args:     []string{"build", "-race=T", "./..."},
			expected: []string{"-race"},
		},
		{
			name:     "race=TRUE is truthy",
			args:     []string{"build", "-race=TRUE", "./..."},
			expected: []string{"-race"},
		},
		{
			name:     "cover=True is truthy",
			args:     []string{"build", "-cover=True", "./..."},
			expected: []string{"-cover"},
		},
		{
			name:     "race=0 is falsy",
			args:     []string{"build", "-race=0", "./..."},
			expected: []string{"-race=false"},
		},
		{
			name:     "race=f is falsy",
			args:     []string{"build", "-race=f", "./..."},
			expected: []string{"-race=false"},
		},
		{
			name:     "race=FALSE is falsy",
			args:     []string{"build", "-race=FALSE", "./..."},
			expected: []string{"-race=false"},
		},
		{
			name:     "race=invalid is skipped",
			args:     []string{"build", "-race=invalid", "./..."},
			expected: nil,
		},
		// Override behavior tests - last value wins
		{
			name:     "race then race=false - false wins",
			args:     []string{"build", "-race", "-race=false", "./..."},
			expected: []string{"-race=false"},
		},
		{
			name:     "race=false then race - true wins",
			args:     []string{"build", "-race=false", "-race", "./..."},
			expected: []string{"-race"},
		},
		{
			name:     "race=true then race=false - false wins",
			args:     []string{"build", "-race=true", "-race=false", "./..."},
			expected: []string{"-race=false"},
		},
		{
			name:     "multiple overrides - last wins",
			args:     []string{"build", "-race", "-race=false", "-race=true", "-race=0", "./..."},
			expected: []string{"-race=false"}, // Last is -race=0 which is false
		},
		{
			name:     "cover disabled then enabled with tags",
			args:     []string{"build", "-cover=false", "-tags=foo", "-cover", "./..."},
			expected: []string{"-tags=foo", "-cover"}, // value flags first, then bool
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBuildFlags(tt.args)
			if !slices.Equal(result, tt.expected) {
				t.Errorf("extractBuildFlags(%v) = %v, expected %v", tt.args, result, tt.expected)
			}
		})
	}
}
