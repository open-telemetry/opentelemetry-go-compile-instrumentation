// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
	"golang.org/x/tools/go/packages"
)

func TestGoBuild_RejectsUnsupportedSubcommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no subcommand", args: []string{"go"}},
		{name: "unsupported subcommand run", args: []string{"go", "run", "./..."}},
		{name: "unsupported subcommand vet", args: []string{"go", "vet", "./..."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cli.Command{Name: "go", SkipFlagParsing: true, Action: GoBuild}
			err := cmd.Run(t.Context(), tt.args)
			require.Error(t, err)
			require.Contains(t, err.Error(), "supported")
		})
	}
}

func TestToolexecBuildArgs(t *testing.T) {
	t.Run("inserts -work and the -toolexec ahead of the caller's args", func(t *testing.T) {
		got := toolexecBuildArgs([]string{"build", "-o", "app", "./cmd"}, "/opt/my tools/otelc", false)
		assert.Equal(t, []string{
			"go", "build", "-work",
			"-toolexec=/opt/my tools/otelc toolexec",
			"-o", "app", "./cmd",
		}, got)
	})

	t.Run("neutralizes -mod=vendor when vendored", func(t *testing.T) {
		got := toolexecBuildArgs([]string{"build", "-mod=vendor", "."}, "/usr/bin/otelc", true)
		assert.NotContains(t, got, "-mod=vendor")
	})

	t.Run("adds the generated runtime file for file targets", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, otelcRuntimeFile), []byte("package main\n"), 0o644))

		got := toolexecBuildArgs([]string{"build", "main.go"}, "/usr/bin/otelc", false)
		assert.Contains(t, got, otelcRuntimeFile)
	})

	t.Run("go test inserts runtime file before -- delimiter", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "main_test.go"), []byte("package main\n"), 0o644))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, otelcRuntimeFile), []byte("package main\n"), 0o644))

		got := toolexecBuildArgs(
			[]string{"test", "main.go", "main_test.go", "--", "custom.go"},
			"/usr/bin/otelc",
			false,
		)
		delimIdx := slices.Index(got, "--")
		require.Positive(t, delimIdx)
		runtimeIdx := slices.Index(got, otelcRuntimeFile)
		require.Positive(t, runtimeIdx)
		assert.Equal(t, delimIdx-1, runtimeIdx, "runtime file must appear immediately before --")
	})

	t.Run("go test inserts runtime file before -args delimiter", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, otelcRuntimeFile), []byte("package main\n"), 0o644))

		got := toolexecBuildArgs([]string{"test", "main.go", "-args", "custom.go"}, "/usr/bin/otelc", false)
		delimIdx := slices.Index(got, "-args")
		require.Positive(t, delimIdx)
		runtimeIdx := slices.Index(got, otelcRuntimeFile)
		require.Positive(t, runtimeIdx)
		assert.Equal(t, delimIdx-1, runtimeIdx, "runtime file must appear immediately before -args")
	})

	t.Run("go test inserts runtime file before --args delimiter", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, otelcRuntimeFile), []byte("package main\n"), 0o644))

		got := toolexecBuildArgs([]string{"test", "main.go", "--args", "custom.go"}, "/usr/bin/otelc", false)
		delimIdx := slices.Index(got, "--args")
		require.Positive(t, delimIdx)
		runtimeIdx := slices.Index(got, otelcRuntimeFile)
		require.Positive(t, runtimeIdx)
		assert.Equal(t, delimIdx-1, runtimeIdx, "runtime file must appear immediately before --args")
	})

	t.Run("go test does not inject runtime file for .go files after delimiters", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "custom.go"), []byte("package main\n"), 0o644))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, otelcRuntimeFile), []byte("package main\n"), 0o644))

		for _, delim := range []string{"--", "-args", "--args"} {
			got := toolexecBuildArgs([]string{"test", delim, "custom.go"}, "/usr/bin/otelc", false)
			assert.NotContains(
				t,
				got,
				otelcRuntimeFile,
				"no runtime file should be injected when .go file is after %s",
				delim,
			)
		}
	})

	t.Run("go build preserves existing delimiter and appends runtime file at end", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, otelcRuntimeFile), []byte("package main\n"), 0o644))

		got := toolexecBuildArgs([]string{"build", "--", "main.go"}, "/usr/bin/otelc", false)
		delimIdx := slices.Index(got, "--")
		require.Positive(t, delimIdx)
		runtimeIdx := slices.Index(got, otelcRuntimeFile)
		require.Positive(t, runtimeIdx)
		assert.Greater(t, runtimeIdx, delimIdx, "runtime file should be appended at end for go build --")
	})

	t.Run("go test preserves delimiter-like flag values", func(t *testing.T) {
		for _, val := range []string{"--", "-args", "--args"} {
			dir := t.TempDir()
			t.Chdir(dir)
			require.NoError(t, os.WriteFile(
				filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644))
			require.NoError(t, os.WriteFile(
				filepath.Join(dir, "main_test.go"), []byte("package main\n"), 0o644))
			require.NoError(t, os.WriteFile(
				filepath.Join(dir, otelcRuntimeFile), []byte("package main\n"), 0o644))

			got := toolexecBuildArgs(
				[]string{"test", "main.go", "main_test.go", "-test.run", val},
				"/usr/bin/otelc",
				false,
			)
			runIdx := slices.Index(got, "-test.run")
			require.Positive(t, runIdx)
			require.Equal(t, val, got[runIdx+1], "flag value must immediately follow flag")

			gotWithDelim := toolexecBuildArgs(
				[]string{"test", "main.go", "main_test.go", "-test.run", val, "--", "custom.go"},
				"/usr/bin/otelc",
				false,
			)
			runIdx = slices.Index(gotWithDelim, "-test.run")
			require.Positive(t, runIdx)
			require.Equal(t, val, gotWithDelim[runIdx+1], "flag value must immediately follow flag")
			realDelimIdx := slices.Index(gotWithDelim[runIdx+2:], "--") + runIdx + 2
			runtimeIdx := slices.Index(gotWithDelim, otelcRuntimeFile)
			require.Positive(t, runtimeIdx)
			assert.Equal(t, realDelimIdx-1, runtimeIdx, "runtime file must appear before the real delimiter")
		}
	})
}

func TestFindTestDelimiter(t *testing.T) {
	assert.Equal(t, -1, findTestDelimiter(subcmdBuild, []string{"--", "main.go"}))
	assert.Equal(t, 1, findTestDelimiter(subcmdTest, []string{"./pkg", "--", "custom"}))
	assert.Equal(t, 1, findTestDelimiter(subcmdTest, []string{"./pkg", "-args", "custom"}))
	assert.Equal(t, 1, findTestDelimiter(subcmdTest, []string{"./pkg", "--args", "custom"}))

	for _, delim := range []string{"--", "-args", "--args"} {
		assert.Equal(t, -1, findTestDelimiter(subcmdTest, []string{"-test.run", delim, "./pkg"}))
		assert.Equal(t, -1, findTestDelimiter(subcmdTest, []string{"--test.run", delim, "./pkg"}))
		assert.Equal(t, -1, findTestDelimiter(subcmdTest, []string{"-run", delim, "./pkg"}))
		assert.Equal(t, 2, findTestDelimiter(subcmdTest, []string{"-test.run", delim, "--", "./pkg"}))
		assert.Equal(t, 2, findTestDelimiter(subcmdTest, []string{"-test.run", delim, "-args", "./pkg"}))
		assert.Equal(t, 2, findTestDelimiter(subcmdTest, []string{"-test.run", delim, "--args", "./pkg"}))
	}
}

func TestGetPackages(t *testing.T) {
	setupTestModule(t, []string{"cmd", "foo/demo"})

	tests := []struct {
		name             string
		args             []string
		expectedCount    int
		expectedPackages []string
		expectError      bool
	}{
		{
			name:             "single package",
			args:             []string{"-a", "-o", "tmp", "./cmd"},
			expectedCount:    1,
			expectedPackages: []string{"testmodule/cmd"},
			expectError:      false,
		},
		{
			name:             "multiple packages",
			args:             []string{"./cmd", "./foo/demo"},
			expectedCount:    2,
			expectedPackages: []string{"testmodule/cmd", "testmodule/foo/demo"},
			expectError:      false,
		},
		{
			name:             "wildcard pattern",
			args:             []string{"./cmd/..."},
			expectedCount:    1,
			expectedPackages: []string{"testmodule/cmd"},
			expectError:      false,
		},
		{
			name:             "file as a target",
			args:             []string{"./cmd/main.go"},
			expectedCount:    1,
			expectedPackages: []string{pkgload.CommandLineArgumentsPackage},
			expectError:      false,
		},
		{
			name:             "file and pkg mixed targets",
			args:             []string{"./cmd/main.go", "./foo/demo"},
			expectedCount:    0,
			expectedPackages: []string{},
			expectError:      true,
		},
		{
			name:             "default to current directory",
			args:             []string{},
			expectedCount:    1,
			expectedPackages: []string{"testmodule"},
			expectError:      false,
		},
		{
			name:             "current directory explicit",
			args:             []string{"."},
			expectedCount:    1,
			expectedPackages: []string{"testmodule"},
			expectError:      false,
		},
		{
			name:             "nonexistent package mixed with valid",
			args:             []string{"./cmd", "./nonexistent"},
			expectedCount:    1,
			expectedPackages: []string{"testmodule/cmd"},
			expectError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkgs, err := getBuildPackages(t.Context(), subcmdBuild, tt.args)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Len(t, pkgs, tt.expectedCount)

				if tt.expectedPackages != nil {
					pkgIDs := extractPackageIDs(pkgs)
					checkPackages(t, pkgIDs, tt.expectedPackages)
				}
			}
		})
	}
}

func TestGetPackagesWithChangeDirectoryFlag(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "app")
	require.NoError(t, os.MkdirAll(appDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(appDir, "go.mod"),
		[]byte("module example.com/app\n\ngo 1.21\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(appDir, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"),
		0o644,
	))
	t.Chdir(tmpDir)

	pkgs, err := getBuildPackages(t.Context(), subcmdBuild, []string{"-C", "app", "."})
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	require.NotNil(t, pkgs[0].Module)
	require.Equal(t, "example.com/app", pkgs[0].Module.Path)
}

func TestSplitBuildTargets(t *testing.T) {
	tests := []struct {
		name        string
		subcommand  string
		targets     []string
		pkgTargets  []string
		fileTargets []string
		expectError bool
		wantErr     string
	}{
		{
			name:        "all package targets",
			subcommand:  subcmdBuild,
			targets:     []string{"./cmd", "./foo/demo"},
			pkgTargets:  []string{"./cmd", "./foo/demo"},
			fileTargets: nil,
			expectError: false,
		},
		{
			name:        "all file targets",
			subcommand:  subcmdBuild,
			targets:     []string{"./cmd/main.go", "./cmd/util.go"},
			pkgTargets:  nil,
			fileTargets: []string{"./cmd/main.go", "./cmd/util.go"},
			expectError: false,
		},
		{
			name:        "all file targets from different directories fails",
			subcommand:  subcmdBuild,
			targets:     []string{"./cmd/main.go", "./util/util.go"},
			expectError: true,
			wantErr:     "named files must all be in one directory",
		},
		{
			name:        "mixed package and file targets fails",
			subcommand:  subcmdBuild,
			targets:     []string{"./cmd/main.go", "./foo/demo"},
			expectError: true,
			wantErr:     "cannot mix .go files and packages",
		},
		{
			name:        "go build -o flag requires a value",
			subcommand:  subcmdBuild,
			targets:     []string{"./pkg", "-o"},
			expectError: true,
			wantErr:     `flag "-o" requires a value`,
		},
		{
			name:        "go build -- does not stop file scanning",
			subcommand:  subcmdBuild,
			targets:     []string{"--", "main.go"},
			fileTargets: []string{"main.go"},
			expectError: false,
		},
		{
			name:        "go build -- does not stop package scanning",
			subcommand:  subcmdBuild,
			targets:     []string{"--", "./pkg"},
			pkgTargets:  []string{"./pkg"},
			expectError: false,
		},
		{
			name:        "go build -o out -- ./pkg preserves package",
			subcommand:  subcmdBuild,
			targets:     []string{"-o", "out", "--", "./pkg"},
			pkgTargets:  []string{"./pkg"},
			expectError: false,
		},
		{
			name:        "go build main.go -- other.go scans both files",
			subcommand:  subcmdBuild,
			targets:     []string{"main.go", "--", "other.go"},
			fileTargets: []string{"main.go", "other.go"},
			expectError: false,
		},
		{
			name:        "go build -- cannot mix package and file",
			subcommand:  subcmdBuild,
			targets:     []string{"./pkg", "--", "main.go"},
			expectError: true,
			wantErr:     "cannot mix .go files and packages",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkgTargets, fileTargets, err := splitBuildTargets(tt.subcommand, tt.targets)
			if tt.expectError {
				require.Error(t, err)
				if tt.wantErr != "" {
					require.ErrorContains(t, err, tt.wantErr)
				}
				assert.Nil(t, pkgTargets)
				assert.Nil(t, fileTargets)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.pkgTargets, pkgTargets)
				assert.Equal(t, tt.fileTargets, fileTargets)
			}
		})
	}

	t.Run("flags requiring values fail when value is missing", func(t *testing.T) {
		for _, flag := range []string{
			"-run", "-exec", "-test.run", "--test.run",
			"-test.timeout", "-test.bench", "-test.count",
		} {
			_, _, err := splitBuildTargets(subcmdTest, []string{flag})
			require.ErrorContains(t, err, "requires a value")
		}
	})

	t.Run("supported test flags do not consume packages", func(t *testing.T) {
		flags := []string{
			"-run", "--run", "-test.run", "--test.run",
			"-test.bench", "-test.timeout", "-test.count",
			"-test.cpu", "-test.benchtime", "-vet", "-exec",
		}
		for _, flag := range flags {
			// separated flag value before package
			pkgs, files, err := splitBuildTargets(subcmdTest, []string{flag, "val", "./pkg"})
			require.NoError(t, err)
			assert.Equal(t, []string{"./pkg"}, pkgs)
			assert.Empty(t, files)

			// joined flag value before package
			pkgs, files, err = splitBuildTargets(subcmdTest, []string{flag + "=val", "./pkg"})
			require.NoError(t, err)
			assert.Equal(t, []string{"./pkg"}, pkgs)
			assert.Empty(t, files)

			// package before separated flag
			pkgs, files, err = splitBuildTargets(subcmdTest, []string{"./pkg", flag, "val"})
			require.NoError(t, err)
			assert.Equal(t, []string{"./pkg"}, pkgs)
			assert.Empty(t, files)
		}
	})

	t.Run("boolean and unknown test flags do not consume following argument", func(t *testing.T) {
		for _, flag := range []string{"-test.v", "--test.v", "-test.short", "-test.unknown"} {
			pkgs, files, err := splitBuildTargets(subcmdTest, []string{flag, "./pkg"})
			require.NoError(t, err)
			assert.Equal(t, []string{"./pkg"}, pkgs)
			assert.Empty(t, files)
		}
		pkgs, files, err := splitBuildTargets(subcmdTest, []string{"-test.v=true", "./pkg"})
		require.NoError(t, err)
		assert.Equal(t, []string{"./pkg"}, pkgs)
		assert.Empty(t, files)
	})

	t.Run("test delimiters stop target scanning", func(t *testing.T) {
		delims := []string{"--", "-args", "--args"}
		for _, delim := range delims {
			// package before delimiter, test-binary args after
			pkgs, files, err := splitBuildTargets(subcmdTest, []string{
				"./pkg", delim, "other", "./other/pkg", "foo.go", "-test.run", "val",
			})
			require.NoError(t, err)
			assert.Equal(t, []string{"./pkg"}, pkgs)
			assert.Empty(t, files)

			// no explicit package before delimiter
			pkgs, files, err = splitBuildTargets(subcmdTest, []string{delim, "custom", "foo.go"})
			require.NoError(t, err)
			assert.Empty(t, pkgs)
			assert.Empty(t, files)

			// empty delimiter alone
			pkgs, files, err = splitBuildTargets(subcmdTest, []string{delim})
			require.NoError(t, err)
			assert.Empty(t, pkgs)
			assert.Empty(t, files)

			// files before delimiter, extra after
			pkgs, files, err = splitBuildTargets(subcmdTest, []string{
				"./cmd/main.go", "./cmd/util.go", delim, "extra.go",
			})
			require.NoError(t, err)
			assert.Empty(t, pkgs)
			assert.Equal(t, []string{"./cmd/main.go", "./cmd/util.go"}, files)

			// package before, file after succeeds (not mixed)
			pkgs, files, err = splitBuildTargets(subcmdTest, []string{"./pkg", delim, "main.go"})
			require.NoError(t, err)
			assert.Equal(t, []string{"./pkg"}, pkgs)
			assert.Empty(t, files)

			// file before, package after succeeds (not mixed)
			pkgs, files, err = splitBuildTargets(subcmdTest, []string{"./cmd/main.go", delim, "./pkg"})
			require.NoError(t, err)
			assert.Empty(t, pkgs)
			assert.Equal(t, []string{"./cmd/main.go"}, files)

			// file before, file in different directory after succeeds
			pkgs, files, err = splitBuildTargets(subcmdTest, []string{
				"./cmd/main.go", delim, "./util/util.go",
			})
			require.NoError(t, err)
			assert.Empty(t, pkgs)
			assert.Equal(t, []string{"./cmd/main.go"}, files)
		}

		// mixed package and file before delimiter still fails
		_, _, err := splitBuildTargets(subcmdTest, []string{
			"./cmd/main.go", "./pkg", "--", "custom",
		})
		require.ErrorContains(t, err, "cannot mix .go files and packages")

		// multiple file targets in different dirs before delimiter still fails
		_, _, err = splitBuildTargets(subcmdTest, []string{
			"./cmd/main.go", "./util/util.go", "--", "custom",
		})
		require.ErrorContains(t, err, "named files must all be in one directory")
	})

	t.Run("repeated delimiters and delimiter flag values", func(t *testing.T) {
		for _, delim1 := range []string{"--", "-args", "--args"} {
			for _, delim2 := range []string{"--", "-args", "--args"} {
				pkgs, files, err := splitBuildTargets(subcmdTest, []string{
					"./pkg", delim1, delim2, "custom",
				})
				require.NoError(t, err)
				assert.Equal(t, []string{"./pkg"}, pkgs)
				assert.Empty(t, files)
			}
		}

		// delimiter used as flag value does not stop scanning
		for _, val := range []string{"--", "-args", "--args"} {
			pkgs, files, err := splitBuildTargets(subcmdTest, []string{"-test.run", val, "./pkg"})
			require.NoError(t, err)
			assert.Equal(t, []string{"./pkg"}, pkgs)
			assert.Empty(t, files)
		}
	})
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

	mainGoPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainGoPath, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("Failed to create main.go: %v", err)
	}

	t.Chdir(tmpDir)
}

func TestRootModulePaths(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "app")
	require.NoError(t, os.MkdirAll(appDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, "go.mod"),
		[]byte("module example.com/app\n\ngo 1.25\n"),
		0o644,
	))
	t.Chdir(tmpDir)
	mainFile := filepath.Join(appDir, "main.go")
	require.NoError(t, os.WriteFile(mainFile, []byte("package main\n\nfunc main() {}\n"), 0o644))

	got, err := rootModulePaths(t.Context(), []*packages.Package{
		{PkgPath: "example.com/direct", Module: &packages.Module{Path: "example.com/direct"}},
		{PkgPath: pkgload.CommandLineArgumentsPackage, GoFiles: []string{mainFile}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"example.com/app", "example.com/direct"}, got)
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

	t.Run("creates persistent cache in .otelc-build/gocache", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, tempDir)
		if err := os.MkdirAll(util.GetBuildTempDir(), 0o755); err != nil {
			t.Fatal(err)
		}

		env, err := setupGoCache(t.Context(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var cacheDir string
		for _, e := range env {
			if suffix, ok := strings.CutPrefix(e, "GOCACHE="); ok {
				cacheDir = suffix
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
			name:     "trimpath flag",
			args:     []string{"build", "-trimpath", "./..."},
			expected: []string{"-trimpath"},
		},
		{
			name:     "trimpath false",
			args:     []string{"build", "-trimpath=false", "./..."},
			expected: []string{"-trimpath=false"},
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
			name:     "change directory flag",
			args:     []string{"build", "-C", "app", "./..."},
			expected: []string{"-C", "app"},
		},
		{
			name:     "overlay flag",
			args:     []string{"build", "-overlay=overlay.json", "./..."},
			expected: []string{"-overlay=overlay.json"},
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

func TestIsSetup(t *testing.T) {
	// isSetup is currently a stub that always reports false.
	assert.False(t, isSetup())
}

// TestSetupPhaseLogDelegators exercises the thin slog delegators on SetupPhase.
// They must forward to the underlying logger without panicking.
func TestSetupPhaseLogDelegators(t *testing.T) {
	sp := newTestSetupPhase()
	assert.NotPanics(t, func() {
		sp.Info("info", "k", "v")
		sp.Warn("warn", "k", "v")
		sp.Error("error", "k", "v")
		sp.Debug("debug", "k", "v")
	})
}

func TestGenerateRuntimePerPackageSkipsPackagesWithoutFiles(t *testing.T) {
	sp := newTestSetupPhase()

	// A package with no Go files has an empty package directory and must be
	// skipped without error.
	pkgs := []*packages.Package{{PkgPath: "example.com/empty"}}
	err := sp.generateRuntimePerPackage(context.Background(), pkgs, []*rule.InstRuleSet{})
	require.NoError(t, err)
}

func TestGetBuildPackages_LoadErrors(t *testing.T) {
	ctx := t.Context()
	nonExistentDir := filepath.Join(t.TempDir(), "nonexistent")

	// File targets with non-existent -C flag
	_, err := getBuildPackages(ctx, subcmdBuild, []string{"-C", nonExistentDir, "main.go"})
	require.Error(t, err)

	// Package targets with non-existent -C flag
	_, err = getBuildPackages(ctx, subcmdBuild, []string{"-C", nonExistentDir, "./pkg"})
	require.Error(t, err)

	// Default targets with non-existent -C flag
	_, err = getBuildPackages(ctx, subcmdBuild, []string{"-C", nonExistentDir})
	require.Error(t, err)
}

func TestRootModulePaths_ResolveError(t *testing.T) {
	ctx := t.Context()
	pkgs := []*packages.Package{
		{
			PkgPath: "example.com/foo",
			GoFiles: []string{filepath.Join(t.TempDir(), "nonexistent", "foo.go")},
		},
	}
	_, err := rootModulePaths(ctx, pkgs)
	require.Error(t, err)
}

func TestGenerateRuntimePerPackage_AddDepsError(t *testing.T) {
	sp := newTestSetupPhase()
	nonExistentDir := filepath.Join(t.TempDir(), "nonexistent")

	pkgs := []*packages.Package{
		{
			PkgPath: "example.com/foo",
			Name:    "foo",
			GoFiles: []string{filepath.Join(nonExistentDir, "foo.go")},
		},
	}
	rset := rule.NewInstRuleSet("example.com/foo")
	rset.FuncRules["foo.go"] = []*rule.InstFuncRule{
		{
			InstBaseRule: rule.InstBaseRule{Name: "test-rule"},
			Func:         "Foo",
			Before:       "BeforeFoo",
			Path:         "example.com/hook",
		},
	}
	err := sp.generateRuntimePerPackage(context.Background(), pkgs, []*rule.InstRuleSet{rset})
	require.Error(t, err)
}

func TestSetup_AutoPinError(t *testing.T) {
	setupTestModule(t, []string{"cmd"})

	// Make stateDir a regular file so autoPin fails in setupLocked on all platforms (line 392)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	snapshotDir := util.GetBuildTemp(stateDir)
	_ = os.RemoveAll(snapshotDir)
	require.NoError(t, os.WriteFile(snapshotDir, []byte("file"), 0o644))

	cmd := &cli.Command{
		Name:   "setup",
		Action: Setup,
	}
	err := cmd.Run(t.Context(), []string{"setup", "."})
	require.Error(t, err)
}

func TestSetup_FindDepsErrorWithRules(t *testing.T) {
	setupTestModule(t, []string{"cmd"})
	t.Setenv(util.EnvOtelcRules, "some-rule-config")

	// Ensure build temp dir is a regular file so listBuildPlan in findDeps fails (line 401)
	_ = os.RemoveAll(util.GetBuildTempDir())
	require.NoError(t, os.WriteFile(util.GetBuildTempDir(), []byte("file"), 0o644))

	cmd := &cli.Command{
		Name:   "setup",
		Action: Setup,
	}
	err := cmd.Run(t.Context(), []string{"setup", "."})
	require.Error(t, err)
}

func TestSetup_MatchDepsError(t *testing.T) {
	setupTestModule(t, []string{"cmd"})
	t.Setenv(util.EnvOtelcRules, "/nonexistent/rules.yaml")
	_ = os.RemoveAll(util.GetBuildTempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	cmd := &cli.Command{
		Name:   "setup",
		Action: Setup,
	}
	err := cmd.Run(t.Context(), []string{"setup", "."})
	require.Error(t, err)
}

func TestSetupLocked_FindModuleDirsError(t *testing.T) {
	// A standalone .go file outside any Go module causes FindModuleDirs to fail in setupLocked (line 377)
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)

	mainFile := filepath.Join(tmp, "main.go")
	mustWriteFile(t, mainFile, "package main\nfunc main() {}\n")

	cmd := &cli.Command{
		Name:   "setup",
		Action: Setup,
	}
	err := cmd.Run(t.Context(), []string{"setup", mainFile})
	require.Error(t, err)
}
