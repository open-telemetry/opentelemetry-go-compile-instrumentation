// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	plan := os.Getenv("GO_HELPER_BUILD_PLAN")
	if plan != "" {
		fmt.Fprintln(os.Stderr, plan)
	}
	if os.Getenv("GO_HELPER_BUILD_FAILS") == "1" {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestParseCdDir(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		expectedDir string
		expectedOk  bool
	}{
		{
			name:        "valid cd command",
			line:        "cd /home/user/project",
			expectedDir: "/home/user/project",
			expectedOk:  true,
		},
		{
			name:        "cd command with spaces",
			line:        "cd /tmp/test project with spaces",
			expectedDir: "/tmp/test project with spaces",
			expectedOk:  true,
		},
		{
			name:        "cd command with hash in path",
			line:        "cd /home/user/project # build comment",
			expectedDir: "/home/user/project # build comment",
			expectedOk:  true,
		},
		{
			name:        "uppercase CD command",
			line:        "CD /home/user/project",
			expectedDir: "/home/user/project",
			expectedOk:  true,
		},
		{
			name:        "cd with Windows path containing spaces",
			line:        "cd C:\\Users\\test user\\project",
			expectedDir: "C:\\Users\\test user\\project",
			expectedOk:  true,
		},
		{
			name:        "cd with trailing whitespace",
			line:        "cd /home/user/project  \r",
			expectedDir: "/home/user/project",
			expectedOk:  true,
		},
		{
			name:       "cd without directory",
			line:       "cd",
			expectedOk: false,
		},
		{
			name:       "cd with empty directory",
			line:       "cd   ",
			expectedOk: false,
		},
		{
			name:       "command beginning with cd",
			line:       "cdrom /home/user/project",
			expectedOk: false,
		},
		{
			name:        "not a cd command",
			line:        "compile -o output.a main.go",
			expectedDir: "",
			expectedOk:  false,
		},
		{
			name:        "empty line",
			line:        "",
			expectedDir: "",
			expectedOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, ok := parseCdDir(tt.line)
			assert.Equal(t, tt.expectedOk, ok)
			assert.Equal(t, tt.expectedDir, dir)
		})
	}
}

func TestResolveCgoFile(t *testing.T) {
	tests := []struct {
		name       string
		cgoFile    string
		createFile string
		wantErr    bool
	}{
		{
			name:       "valid cgo file with source dir",
			cgoFile:    "$WORK/b001/main.cgo1.go",
			createFile: "main.go",
			wantErr:    false,
		},
		{
			name:       "valid cgo file in subdirectory",
			cgoFile:    "/tmp/work/subpkg/handler.cgo1.go",
			createFile: "handler.go",
			wantErr:    false,
		},
		{
			name:    "not a cgo file",
			cgoFile: "main.go",
			wantErr: true,
		},
		{
			name:    "cgo file but original does not exist in source dir",
			cgoFile: "missing.cgo1.go",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			if tt.createFile != "" {
				err := os.WriteFile(filepath.Join(tmpDir, tt.createFile), []byte("package main"), 0o644)
				require.NoError(t, err)
			}

			goFile, err := resolveCgoFile(tt.cgoFile, tmpDir)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			expectedPath, err1 := filepath.EvalSymlinks(filepath.Join(tmpDir, tt.createFile))
			require.NoError(t, err1)
			gotPath, err2 := filepath.EvalSymlinks(goFile)
			require.NoError(t, err2)
			assert.Equal(t, expectedPath, gotPath)
		})
	}
}

func TestResolveCgoFile_EmptyParams(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("empty sourceDir returns error", func(t *testing.T) {
		_, err := resolveCgoFile("server.cgo1.go", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("empty cgoFile returns error", func(t *testing.T) {
		_, err := resolveCgoFile("", tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})
}

func TestFindCommands(t *testing.T) {
	tests := []struct {
		name             string
		buildPlanContent string
		expectedCommands []string
	}{
		{
			name:             "empty build plan",
			buildPlanContent: "",
			expectedCommands: nil,
		},
		{
			name:             "single compile command",
			buildPlanContent: `/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/out.a -p main -buildid abc main.go`,
			expectedCommands: []string{
				"/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/out.a -p main -buildid abc main.go",
			},
		},
		{
			name: "multiple compile commands",
			buildPlanContent: `
/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/pkg1.a -p pkg1 -buildid abc1 pkg1.go
/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/pkg2.a -p pkg2 -buildid abc2 pkg2.go
/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/main.a -p main -buildid abc3 main.go
`,
			expectedCommands: []string{
				"/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/pkg1.a -p pkg1 -buildid abc1 pkg1.go",
				"/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/pkg2.a -p pkg2 -buildid abc2 pkg2.go",
				"/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/main.a -p main -buildid abc3 main.go",
			},
		},
		{
			name: "cd and cgo commands included",
			buildPlanContent: `
cd /home/user/project/pkg/cgopkg
/usr/local/go/pkg/tool/darwin_arm64/cgo -objdir /tmp/go-build123/b001 -importpath github.com/example/cgopkg
/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/go-build123/b001/out.a -p github.com/example/cgopkg -buildid xyz file.cgo1.go
`,
			expectedCommands: []string{
				"cd /home/user/project/pkg/cgopkg",
				"/usr/local/go/pkg/tool/darwin_arm64/cgo -objdir /tmp/go-build123/b001 -importpath github.com/example/cgopkg",
				"/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/go-build123/b001/out.a -p github.com/example/cgopkg -buildid xyz file.cgo1.go",
			},
		},
		{
			name: "cd path with spaces included",
			buildPlanContent: `
cd /tmp/test project with spaces
/usr/local/go/pkg/tool/darwin_arm64/cgo -objdir /tmp/go-build123/b001 -importpath github.com/example/cgopkg
/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/go-build123/b001/out.a -p github.com/example/cgopkg -buildid xyz file.cgo1.go
`,
			expectedCommands: []string{
				"cd /tmp/test project with spaces",
				"/usr/local/go/pkg/tool/darwin_arm64/cgo -objdir /tmp/go-build123/b001 -importpath github.com/example/cgopkg",
				"/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/go-build123/b001/out.a -p github.com/example/cgopkg -buildid xyz file.cgo1.go",
			},
		},
		{
			name: "malformed cd commands ignored",
			buildPlanContent: `
cd
` + "cd   \n" + `
cdrom
cdrom /project/src
`,
			expectedCommands: nil,
		},
		{
			name: "multiple cgo packages",
			buildPlanContent: `
cd /project/pkg/cgo1
/usr/local/go/pkg/tool/darwin_arm64/cgo -objdir /tmp/build/b001 -importpath pkg/cgo1
cd /project/pkg/cgo2
/usr/local/go/pkg/tool/darwin_arm64/cgo -objdir /tmp/build/b002 -importpath pkg/cgo2
/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/build/b001/out.a -p pkg/cgo1 -buildid a file.go
/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/build/b002/out.a -p pkg/cgo2 -buildid b file.go
`,
			expectedCommands: []string{
				"cd /project/pkg/cgo1",
				"/usr/local/go/pkg/tool/darwin_arm64/cgo -objdir /tmp/build/b001 -importpath pkg/cgo1",
				"cd /project/pkg/cgo2",
				"/usr/local/go/pkg/tool/darwin_arm64/cgo -objdir /tmp/build/b002 -importpath pkg/cgo2",
				"/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/build/b001/out.a -p pkg/cgo1 -buildid a file.go",
				"/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/build/b002/out.a -p pkg/cgo2 -buildid b file.go",
			},
		},
		{
			name: "skip pgo compile commands",
			buildPlanContent: `
/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/out.a -p main -buildid abc -pgoprofile /tmp/profile.pgo main.go
/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/out2.a -p main -buildid def main.go
`,
			expectedCommands: []string{
				"/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/out2.a -p main -buildid def main.go",
			},
		},
		{
			name: "cgo dynimport should be ignored",
			buildPlanContent: `
cd /project/pkg/cgo
/usr/local/go/pkg/tool/darwin_arm64/cgo -dynimport /tmp/build/_cgo_.o -objdir /tmp/build/b001 -importpath pkg/cgo
/usr/local/go/pkg/tool/darwin_arm64/cgo -objdir /tmp/build/b001 -importpath pkg/cgo
`,
			expectedCommands: []string{
				"cd /project/pkg/cgo",
				"/usr/local/go/pkg/tool/darwin_arm64/cgo -objdir /tmp/build/b001 -importpath pkg/cgo",
			},
		},
		{
			name: "filters non-relevant lines",
			buildPlanContent: `
# comment line
mkdir -p /tmp/build
cd /project/src
echo "Building..."
/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/out.a -p main -buildid xyz main.go
/usr/local/go/pkg/tool/darwin_arm64/link -o /tmp/output -importcfg /tmp/importcfg
`,
			expectedCommands: []string{
				"cd /project/src",
				"/usr/local/go/pkg/tool/darwin_arm64/compile.exe -o /tmp/out.a -p main -buildid xyz main.go",
			},
		},
		{
			name: "windows style paths",
			buildPlanContent: `
cd C:/Users/test/project/pkg
C:/Go/pkg/tool/windows_amd64/cgo.exe -objdir C:/tmp/build/b001 -importpath pkg/cgo
C:/Go/pkg/tool/windows_amd64/compile.exe -o C:/tmp/out.a -p main -buildid abc main.go
`,
			expectedCommands: []string{
				"cd C:/Users/test/project/pkg",
				"C:/Go/pkg/tool/windows_amd64/cgo.exe -objdir C:/tmp/build/b001 -importpath pkg/cgo",
				"C:/Go/pkg/tool/windows_amd64/compile.exe -o C:/tmp/out.a -p main -buildid abc main.go",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp(t.TempDir(), "build-plan-*.log")
			require.NoError(t, err)
			defer tmpFile.Close()

			_, err = tmpFile.WriteString(tt.buildPlanContent)
			require.NoError(t, err)

			commands, err := findCommands(tmpFile)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedCommands, commands)
		})
	}
}

func TestFindDepsResolvesCgoSourceFromSpacedDirectory(t *testing.T) {
	oldExec := execCommandContext
	t.Cleanup(func() {
		execCommandContext = oldExec
	})

	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	sourceDir := filepath.Join(workDir, "source with spaces")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	sourceFile := filepath.Join(sourceDir, "sample.go")
	require.NoError(t, os.WriteFile(sourceFile, []byte("package sample\n"), 0o644))

	objDir := filepath.Join(workDir, "go-build", "b001")
	generatedFile := filepath.Join(objDir, "sample.cgo1.go")
	buildPlan := fmt.Sprintf(`
cd %s
.../cgo -objdir "%s" -importpath example.com/sample
.../compile -o "%s" -p example.com/sample -buildid test "%s"
`,
		filepath.ToSlash(sourceDir),
		filepath.ToSlash(objDir),
		filepath.ToSlash(filepath.Join(objDir, "_pkg_.a")),
		filepath.ToSlash(generatedFile),
	)

	exe, err := os.Executable()
	require.NoError(t, err)
	execCommandContext = func(
		ctx context.Context,
		name string,
		args ...string,
	) *exec.Cmd {
		assert.Equal(t, "go", name)
		assert.Equal(t, []string{"build", "-a", "-x", "-n", "./..."}, args)

		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestHelperProcess$")
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			"GO_HELPER_BUILD_PLAN="+buildPlan,
		)
		return cmd
	}

	deps, err := findDeps(t.Context(), subcmdBuild, []string{"./..."})
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, "example.com/sample", deps[0].ImportPath)
	require.Len(t, deps[0].Sources, 1)

	expectedSource, err := filepath.EvalSymlinks(sourceFile)
	require.NoError(t, err)
	actualSource, err := filepath.EvalSymlinks(deps[0].Sources[0])
	require.NoError(t, err)
	assert.Equal(t, expectedSource, actualSource)
	assert.Equal(t, "sample.cgo1.go", deps[0].CgoFiles[deps[0].Sources[0]])
}

func TestListBuildPlan(t *testing.T) {
	oldExec := execCommandContext
	defer func() {
		execCommandContext = oldExec
	}()

	tests := []struct {
		name          string
		subcommand    string // defaults to "build" when empty
		buildPlan     string
		args          []string
		expected      []string
		wantErr       bool
		buildFails    bool
		expectedGoCmd []string
	}{
		{
			name: "filters compile and cgo commands",
			buildPlan: `
cd /project/pkg
.../cgo -objdir /tmp/b001 -importpath pkg/cgo
.../compile -o /tmp/out.a -buildid abc -p main main.go
echo ignored
`,
			args: []string{"./..."},
			expected: []string{
				"cd /project/pkg",
				".../cgo -objdir /tmp/b001 -importpath pkg/cgo",
				".../compile -o /tmp/out.a -buildid abc -p main main.go",
			},
			expectedGoCmd: []string{
				"build", "-a", "-x", "-n", "./...",
			},
		},
		{
			name: "passes additional build args",
			buildPlan: `
.../compile -o /tmp/out.a -buildid abc -p main main.go
`,
			args: []string{"-tags=integration", "./cmd"},
			expected: []string{
				".../compile -o /tmp/out.a -buildid abc -p main main.go",
			},
			expectedGoCmd: []string{
				"build", "-a", "-x", "-n",
				"-tags=integration",
				"./cmd",
			},
		},
		{
			name: "returns build failure",
			buildPlan: `
go: module example.com missing
`,
			args:       []string{"./bad"},
			buildFails: true,
			wantErr:    true,
			expectedGoCmd: []string{
				"build", "-a", "-x", "-n", "./bad",
			},
		},
		{
			name: "empty build plan",
			buildPlan: `
echo nothing useful
`,
			args: []string{"./..."},
			expectedGoCmd: []string{
				"build", "-a", "-x", "-n", "./...",
			},
		},
		{
			name: "ignores malformed compile lines",
			buildPlan: `
.../compile foo
.../cgo blah
`,
			args:          []string{"./..."},
			expected:      nil,
			expectedGoCmd: []string{"build", "-a", "-x", "-n", "./..."},
		},
		{
			// The test subcommand must list a `go test` plan, which surfaces the
			// test-augmented, external test, and test-main compiles that is_test
			// gates on. A `go build` plan would never contain them.
			name:       "test subcommand lists a go test plan",
			subcommand: "test",
			buildPlan: `
.../compile -o /tmp/out.a -buildid abc -p main main.go
`,
			args: []string{"./..."},
			expected: []string{
				".../compile -o /tmp/out.a -buildid abc -p main main.go",
			},
			expectedGoCmd: []string{"test", "-a", "-x", "-n", "./..."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			err := os.Mkdir(filepath.Join(tempDir, util.BuildTempDir), 0o755)
			require.NoError(t, err)

			t.Setenv(util.EnvOtelcWorkDir, tempDir)

			exe, err := os.Executable()
			require.NoError(t, err)

			execCommandContext = func(
				ctx context.Context,
				name string,
				args ...string,
			) *exec.Cmd {
				assert.Equal(t, "go", name)
				assert.Equal(t, tt.expectedGoCmd, args)

				cmd := exec.CommandContext(ctx, exe, "-test.run=^TestHelperProcess$")
				cmd.Env = append(os.Environ(),
					"GO_WANT_HELPER_PROCESS=1",
					"GO_HELPER_BUILD_PLAN="+tt.buildPlan,
				)
				if tt.buildFails {
					cmd.Env = append(cmd.Env, "GO_HELPER_BUILD_FAILS=1")
				}
				return cmd
			}

			subcommand := tt.subcommand
			if subcommand == "" {
				subcommand = "build"
			}
			buildPlan, err := listBuildPlan(t.Context(), subcommand, tt.args)
			if tt.wantErr {
				require.Error(t, err)
				if tt.buildPlan != "" {
					assert.Contains(t, err.Error(), strings.TrimSpace(tt.buildPlan))
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, buildPlan)
			}
		})
	}
}

func TestFindModVersion(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "module cache path",
			path: "/go/pkg/mod/github.com/foo/bar@v1.2.3/pkg/foo.go",
			want: "v1.2.3",
		},
		{
			name: "module cache path with pre-release",
			path: "/go/pkg/mod/github.com/foo/bar@v1.2.3-rc.1/pkg/foo.go",
			want: "v1.2.3-rc.1",
		},
		{
			name: "windows-style module cache path",
			// Use /-separated form so this exercises the same path shape
			// filepath.ToSlash produces on Windows, without depending on GOOS
			// (filepath.ToSlash is a no-op when the host separator is already /).
			path: "C:/go/pkg/mod/github.com/foo/bar@v9.0.0/client.go",
			want: "v9.0.0",
		},
		{
			name: "local path has no version",
			path: "/home/user/projects/bar/pkg/foo.go",
			want: "",
		},
		{
			name: "vendor path has no version",
			path: "/tmp/myapp/vendor/github.com/foo/bar/pkg/foo.go",
			want: "",
		},
		{
			name: "empty path",
			path: "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, findModVersion(tt.path))
		})
	}
}
