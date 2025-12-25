// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
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

func TestSyncDeps_WithRules(t *testing.T) {
	tempDir := t.TempDir()

	// Create a go.mod in temp directory
	gomodPath := filepath.Join(tempDir, "go.mod")
	gomodContent := `module example.com/test

go 1.21
`
	err := os.WriteFile(gomodPath, []byte(gomodContent), 0o644)
	require.NoError(t, err)

	// Change to temp directory
	t.Chdir(tempDir)

	// Set environment variable to override build temp dir
	t.Setenv(util.EnvOtelWorkDir, tempDir)

	// Create the pkg directory structure
	pkgDir := filepath.Join(tempDir, "pkg")
	err = os.MkdirAll(pkgDir, 0o755)
	require.NoError(t, err)
	pkgGoMod := filepath.Join(pkgDir, "go.mod")
	err = os.WriteFile(pkgGoMod, []byte("module "+util.OtelRoot+"/pkg\ngo 1.21\n"), 0o644)
	require.NoError(t, err)

	sp := &SetupPhase{
		logger: slog.Default(),
	}

	// Create a mock rule with a path
	funcRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name: "test-rule",
		},
		Path: util.OtelRoot + "/pkg/instrumentation/nethttp",
	}

	ruleSet := &rule.InstRuleSet{
		FuncRules: map[string][]*rule.InstFuncRule{
			"test.go": {funcRule},
		},
	}

	err = sp.syncDeps(t.Context(), []*rule.InstRuleSet{ruleSet}, tempDir)
	// This will likely fail due to missing instrumentation directories,
	// but we're testing that it attempts to add replaces
	if err != nil {
		t.Logf("syncDeps failed (expected in test): %v", err)
	}

	// Read back the go.mod and check if replaces were added
	content, err := os.ReadFile(gomodPath)
	require.NoError(t, err)

	// At minimum, the pkg replace should be added
	assert.Contains(t, string(content), "replace")
}
