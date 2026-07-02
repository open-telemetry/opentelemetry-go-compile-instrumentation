// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

func TestParseGoMod(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expectError bool
		validate    func(*testing.T, *modfile.File)
	}{
		{
			name: "valid go.mod",
			content: `module example.com/test

go 1.21

require (
	github.com/stretchr/testify v1.8.4
)
`,
			expectError: false,
			validate: func(t *testing.T, mf *modfile.File) {
				assert.Equal(t, "example.com/test", mf.Module.Mod.Path)
				assert.Len(t, mf.Require, 1)
				assert.Equal(t, "github.com/stretchr/testify", mf.Require[0].Mod.Path)
			},
		},
		{
			name: "minimal go.mod",
			content: `module example.com/minimal

go 1.21
`,
			expectError: false,
			validate: func(t *testing.T, mf *modfile.File) {
				assert.Equal(t, "example.com/minimal", mf.Module.Mod.Path)
				assert.Empty(t, mf.Require)
			},
		},
		{
			name: "go.mod with replace",
			content: `module example.com/test

go 1.21

require (
	github.com/example/lib v1.0.0
)

replace github.com/example/lib => ../local/lib
`,
			expectError: false,
			validate: func(t *testing.T, mf *modfile.File) {
				assert.Len(t, mf.Replace, 1)
				assert.Equal(t, "github.com/example/lib", mf.Replace[0].Old.Path)
				assert.Equal(t, "../local/lib", mf.Replace[0].New.Path)
			},
		},
		{
			name: "invalid syntax",
			content: `module example.com/test
go 1.21
require (
	github.com/stretchr/testify
)
`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			gomodPath := filepath.Join(tempDir, "go.mod")
			err := os.WriteFile(gomodPath, []byte(tt.content), 0o644)
			require.NoError(t, err)

			mf, err := parseGoMod(gomodPath)
			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, mf)
			if tt.validate != nil {
				tt.validate(t, mf)
			}
		})
	}
}

func TestParseGoMod_MissingFile(t *testing.T) {
	_, err := parseGoMod("/nonexistent/go.mod")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read go.mod file")
}

func TestWriteGoMod(t *testing.T) {
	tempDir := t.TempDir()
	gomodPath := filepath.Join(tempDir, "go.mod")

	// Create a modfile
	mf := &modfile.File{}
	mf.AddModuleStmt("example.com/test")
	mf.AddGoStmt("1.21")
	err := mf.AddRequire("github.com/stretchr/testify", "v1.8.4")
	require.NoError(t, err)

	// Write it
	err = writeGoMod(gomodPath, mf)
	require.NoError(t, err)

	// Read it back and verify
	content, err := os.ReadFile(gomodPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "module example.com/test")
	assert.Contains(t, string(content), "go 1.21")
	assert.Contains(t, string(content), "github.com/stretchr/testify")
}

func TestRunModTidy(t *testing.T) {
	// Create a temporary directory with a valid go.mod
	tempDir := t.TempDir()
	gomodPath := filepath.Join(tempDir, "go.mod")
	gomodContent := `module example.com/test

go 1.21
`
	err := os.WriteFile(gomodPath, []byte(gomodContent), 0o644)
	require.NoError(t, err)

	// Change to temp directory
	t.Chdir(tempDir)

	err = runModTidy(t.Context(), tempDir)
	// This might fail if go is not available or if the environment is weird,
	// but we're mainly testing that the function doesn't crash
	// In a real environment, this should succeed
	if err != nil {
		t.Logf("go mod tidy failed (may be expected in test environment): %v", err)
	}
}

func TestSyncDeps_NoRules(t *testing.T) {
	tempDir := t.TempDir()
	sp := &SetupPhase{
		logger: slog.Default(),
	}

	err := sp.syncDeps(t.Context(), []*rule.InstRuleSet{}, tempDir)
	assert.NoError(t, err)
}

//nolint:revive // if we add named returns then nonamedreturns will complain
func setupSyncDepsTest(t *testing.T, goMod string, instPaths []string) (string, string, string) {
	tempDir := t.TempDir()

	goModPath := filepath.Join(tempDir, "go.mod")
	require.NoError(t, os.WriteFile(goModPath, []byte(goMod), 0o644))

	t.Chdir(tempDir)
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	buildTempDir := util.GetBuildTempDir()

	pkgDir := filepath.Join(buildTempDir, unzippedPkgDir)
	pkgRuntimeDir := filepath.Join(pkgDir, "runtime")
	instDir := filepath.Join(buildTempDir, unzippedInstDir)

	require.NoError(t, os.MkdirAll(pkgRuntimeDir, 0o755))
	require.NoError(t, os.MkdirAll(instDir, 0o755))

	require.NoError(t, os.WriteFile(
		filepath.Join(pkgDir, "go.mod"),
		[]byte("module "+util.OtelcPkgRoot+"\ngo 1.21\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(instDir, "go.mod"),
		[]byte("module "+util.OtelcInstRoot+"\ngo 1.21\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(pkgRuntimeDir, "go.mod"),
		[]byte("module "+util.OtelcPkgRoot+"/runtime\ngo 1.21\n"),
		0o644,
	))

	for _, path := range instPaths {
		instPath := filepath.Join(instDir, path)
		require.NoError(t, os.MkdirAll(instPath, 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(instPath, "go.mod"),
			[]byte("module "+util.OtelcInstRoot+"/"+path+"\ngo 1.21\n"),
			0o644,
		))
	}

	return tempDir, buildTempDir, goModPath
}

func TestSyncDeps_WithRules(t *testing.T) {
	tests := []struct {
		name      string
		funcRules []*rule.InstFuncRule
		fileRules []*rule.InstFileRule
		instPaths []string
	}{
		{
			name: "func rule",
			funcRules: []*rule.InstFuncRule{{
				InstBaseRule: rule.InstBaseRule{Name: "func"},
				ModulePath:   util.OtelcInstRoot + "/net/http/client",
			}},
			instPaths: []string{"net/http/client"},
		},
		{
			name: "file rule",
			fileRules: []*rule.InstFileRule{{
				InstBaseRule: rule.InstBaseRule{Name: "file"},
				ModulePath:   util.OtelcInstRoot + "/runtime",
			}},
			instPaths: []string{"runtime"},
		},
		{
			name: "func and file rule",
			funcRules: []*rule.InstFuncRule{{
				InstBaseRule: rule.InstBaseRule{Name: "func"},
				ModulePath:   util.OtelcInstRoot + "/net/http/client",
			}},
			fileRules: []*rule.InstFileRule{{
				InstBaseRule: rule.InstBaseRule{Name: "file"},
				ModulePath:   util.OtelcInstRoot + "/runtime",
			}},
			instPaths: []string{"net/http/client", "runtime"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goMod := `module example.com/test

go 1.21
`
			tempDir, buildTempDir, goModPath := setupSyncDepsTest(t, goMod, tt.instPaths)

			sp := &SetupPhase{
				logger: slog.Default(),
			}

			ruleSet := &rule.InstRuleSet{
				FileRules: tt.fileRules,
			}
			if len(tt.funcRules) > 0 {
				ruleSet.FuncRules = map[string][]*rule.InstFuncRule{
					"test.go": tt.funcRules,
				}
			}

			require.NoError(t, sp.syncDeps(t.Context(), []*rule.InstRuleSet{ruleSet}, tempDir))

			content, err := os.ReadFile(goModPath)
			require.NoError(t, err)
			got := string(content)

			assert.Contains(t, got, "replace "+util.OtelcPkgRoot+" => "+filepath.Join(buildTempDir, unzippedPkgDir))
			assert.Contains(
				t,
				got,
				"replace "+util.OtelcPkgRoot+"/runtime => "+filepath.Join(buildTempDir, unzippedPkgDir, "runtime"),
			)
			assert.Contains(t, got, "replace "+util.OtelcInstRoot+" => "+filepath.Join(buildTempDir, unzippedInstDir))
			for _, path := range tt.instPaths {
				assert.Contains(t, got,
					"replace "+util.OtelcInstRoot+"/"+path+" => "+filepath.Join(buildTempDir, unzippedInstDir, path))
			}
		})
	}
}

func TestSyncDeps_ExistingReplace(t *testing.T) {
	goMod := fmt.Sprintf(`module example.com/test

go 1.21

replace %s => /already/there
`, util.OtelcInstRoot+"/net/http/client")

	tempDir, buildTempDir, goModPath := setupSyncDepsTest(
		t,
		goMod,
		[]string{"net/http/client"},
	)

	sp := &SetupPhase{
		logger: slog.Default(),
	}

	ruleSet := &rule.InstRuleSet{
		FuncRules: map[string][]*rule.InstFuncRule{
			"test.go": {
				{
					InstBaseRule: rule.InstBaseRule{
						Name: "func",
					},
					ModulePath: util.OtelcInstRoot + "/net/http/client",
				},
			},
		},
	}

	require.NoError(t, sp.syncDeps(t.Context(), []*rule.InstRuleSet{ruleSet}, tempDir))

	content, err := os.ReadFile(goModPath)
	require.NoError(t, err)
	got := string(content)

	assert.Contains(t, got,
		"replace "+util.OtelcInstRoot+"/net/http/client => /already/there")

	assert.NotContains(
		t,
		got,
		"replace "+util.OtelcInstRoot+"/net/http/client => "+filepath.Join(
			buildTempDir,
			unzippedInstDir,
			"net/http/client",
		),
	)

	assert.Equal(t, 1,
		strings.Count(got,
			"replace "+util.OtelcInstRoot+"/net/http/client"))
}

func warnCapture() (*SetupPhase, *bytes.Buffer) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	return &SetupPhase{logger: slog.New(handler)}, &buf
}

func TestSnapshotVersion(t *testing.T) {
	content := `module example.com/app

go 1.22.0

require (
	go.opentelemetry.io/otel v1.38.0
	github.com/example/lib v0.9.0
)

require (
	github.com/indirect/dep v0.5.0 // indirect
)
`
	mf, err := modfile.Parse("go.mod", []byte(content), nil)
	require.NoError(t, err)

	snap := snapshotVersion(mf)

	assert.Equal(t, "1.22.0", snap.goVersion)
	assert.Equal(t, "v1.38.0", snap.deps["go.opentelemetry.io/otel"])
	assert.Equal(t, "v0.9.0", snap.deps["github.com/example/lib"])

	// indirect deps must not leak into the snapshot
	_, tracked := snap.deps["github.com/indirect/dep"]
	assert.False(t, tracked)
}

func TestSnapshotVersion_MinimalGoMod(t *testing.T) {
	content := `module example.com/tiny

go 1.21
`
	mf, err := modfile.Parse("go.mod", []byte(content), nil)
	require.NoError(t, err)

	snap := snapshotVersion(mf)
	assert.Equal(t, "1.21", snap.goVersion)
	assert.Empty(t, snap.deps)
}

func TestWarnVersion_GoVersionRaised(t *testing.T) {
	tests := []struct {
		name      string
		goVersion string
	}{
		{
			name:      "patch version",
			goVersion: "1.22.0",
		},
		{
			name:      "language version",
			goVersion: "1.21",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			gomodPath := filepath.Join(tempDir, "go.mod")
			afterContent := `module example.com/app

go 1.25.0

require (
	go.opentelemetry.io/otel v1.38.0
)
`
			require.NoError(t, os.WriteFile(gomodPath, []byte(afterContent), 0o644))

			sp, buf := warnCapture()
			before := versionSnapshot{
				goVersion: test.goVersion,
				deps: map[string]string{
					"go.opentelemetry.io/otel": "v1.38.0",
				},
			}

			require.NoError(t, sp.warnVersion(gomodPath, before))

			logged := buf.String()
			assert.Contains(t, logged, "bumped go version")
			assert.Contains(t, logged, "old="+test.goVersion)
			assert.Contains(t, logged, "new=1.25.0")
		})
	}
}

func TestWarnVersion_DepVersionRaised(t *testing.T) {
	tempDir := t.TempDir()
	gomodPath := filepath.Join(tempDir, "go.mod")
	afterContent := `module example.com/app

go 1.22.0

require (
	go.opentelemetry.io/otel v1.43.0
)
`
	require.NoError(t, os.WriteFile(gomodPath, []byte(afterContent), 0o644))

	sp, buf := warnCapture()
	before := versionSnapshot{
		goVersion: "1.22.0",
		deps: map[string]string{
			"go.opentelemetry.io/otel": "v1.38.0",
		},
	}

	require.NoError(t, sp.warnVersion(gomodPath, before))

	logged := buf.String()
	assert.Contains(t, logged, "bumped dependency")
	assert.Contains(t, logged, "module=go.opentelemetry.io/otel")
	assert.Contains(t, logged, "old=v1.38.0")
	assert.Contains(t, logged, "new=v1.43.0")
}

func TestWarnVersion_NoChange(t *testing.T) {
	tempDir := t.TempDir()
	gomodPath := filepath.Join(tempDir, "go.mod")
	content := `module example.com/app

go 1.22.0

require (
	go.opentelemetry.io/otel v1.38.0
)
`
	require.NoError(t, os.WriteFile(gomodPath, []byte(content), 0o644))

	sp, buf := warnCapture()
	before := versionSnapshot{
		goVersion: "1.22.0",
		deps: map[string]string{
			"go.opentelemetry.io/otel": "v1.38.0",
		},
	}

	require.NoError(t, sp.warnVersion(gomodPath, before))

	assert.Empty(t, buf.String())
}

func TestWarnVersion_MissingFile(t *testing.T) {
	sp, _ := warnCapture()
	before := versionSnapshot{goVersion: "1.22.0", deps: map[string]string{}}

	err := sp.warnVersion("/nonexistent/go.mod", before)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to check for version bumps")
}

func TestWarnVersion_EmptyGoVersion(t *testing.T) {
	tempDir := t.TempDir()
	gomodPath := filepath.Join(tempDir, "go.mod")
	afterContent := `module example.com/app

go 1.25.0
`
	require.NoError(t, os.WriteFile(gomodPath, []byte(afterContent), 0o644))

	sp, buf := warnCapture()
	before := versionSnapshot{
		goVersion: "",
		deps:      map[string]string{},
	}

	require.NoError(t, sp.warnVersion(gomodPath, before))

	assert.Empty(t, buf.String())
}
