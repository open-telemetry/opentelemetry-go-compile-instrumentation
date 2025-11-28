// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetPackages(t *testing.T) {
	setupTestModule(t, []string{"cmd", "app/vmctl", "pkg/lib"})

	tests := []struct {
		name          string
		args          []string
		expectedCount int
		checkPackages func(t *testing.T, pkgs []string)
	}{
		{
			name:          "single package",
			args:          []string{"build", "-a", "-o", "tmp", "./cmd"},
			expectedCount: 1,
			checkPackages: func(t *testing.T, pkgs []string) {
				if !strings.Contains(pkgs[0], "testmodule/cmd") {
					t.Errorf("Expected package to contain 'testmodule/cmd', got %s", pkgs[0])
				}
			},
		},
		{
			name:          "multiple packages",
			args:          []string{"build", "./cmd", "./app/vmctl"},
			expectedCount: 2,
			checkPackages: func(t *testing.T, pkgs []string) {
				foundCmd, foundVmctl := false, false
				for _, pkg := range pkgs {
					if strings.Contains(pkg, "testmodule/cmd") {
						foundCmd = true
					}
					if strings.Contains(pkg, "testmodule/app/vmctl") {
						foundVmctl = true
					}
				}
				if !foundCmd || !foundVmctl {
					t.Errorf("Expected to find both cmd and vmctl packages, got %v", pkgs)
				}
			},
		},
		{
			name:          "wildcard pattern",
			args:          []string{"build", "./cmd/..."},
			expectedCount: 1,
			checkPackages: func(t *testing.T, pkgs []string) {
				if !strings.Contains(pkgs[0], "testmodule/cmd") {
					t.Errorf("Expected package to contain 'testmodule/cmd', got %s", pkgs[0])
				}
			},
		},
		{
			name:          "default to current directory",
			args:          []string{"build"},
			expectedCount: 1,
			checkPackages: func(t *testing.T, pkgs []string) {
				if pkgs[0] != "." && !strings.Contains(pkgs[0], "testmodule") {
					t.Errorf("Expected root package, got %s", pkgs[0])
				}
			},
		},
		{
			name:          "current directory explicit",
			args:          []string{"build", "."},
			expectedCount: 1,
		},
		{
			name:          "nonexistent package mixed with valid",
			args:          []string{"build", "./cmd", "./nonexistent"},
			expectedCount: 2, // Function loads all patterns, including errored packages.
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkgs, err := GetBuildPackages(tt.args)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(pkgs) != tt.expectedCount {
				t.Errorf("Expected %d packages, got %d", tt.expectedCount, len(pkgs))
			}

			if tt.checkPackages != nil {
				pkgIDs := make([]string, len(pkgs))
				for i, pkg := range pkgs {
					pkgIDs[i] = pkg.ID
				}
				tt.checkPackages(t, pkgIDs)
			}
		})
	}
}

// setupTestModule creates a temporary Go module with the given subdirectories.
// Each subdirectory will contain a simple main.go file.
// Returns cleanup function to restore the original working directory.
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

	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	t.Cleanup(func() {
		_ = os.Chdir(originalWd)
	})
}
