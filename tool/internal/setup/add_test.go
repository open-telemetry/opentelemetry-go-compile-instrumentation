// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

// Package setup tests verify that the addDeps function generates
// the expected otel.runtime.go file by comparing against golden files.
//
// To update golden files after intentional changes:
//
//	go test -update ./tool/internal/setup/...

package setup

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gotest.tools/v3/golden"
)

func TestAddDeps(t *testing.T) {
	tests := []struct {
		name       string
		matched    []*rule.InstRuleSet
		goldenFile string // Empty means no file should be generated
	}{
		{
			name:       "empty_matched_rules",
			matched:    []*rule.InstRuleSet{},
			goldenFile: "",
		},
		{
			name: "single_func_rule",
			matched: []*rule.InstRuleSet{
				newTestRuleSet(
					"github.com/example/pkg",
					newTestFuncRule("github.com/example/pkg", "github.com/example/pkg"),
				),
			},
			goldenFile: "single_func_rule.otel.runtime.go.golden",
		},
		{
			name: "no_func_rules",
			matched: []*rule.InstRuleSet{
				newTestRuleSet("github.com/example/pkg"),
			},
			goldenFile: "",
		},
		{
			name: "multiple_rule_sets",
			matched: []*rule.InstRuleSet{
				newTestRuleSet(
					"github.com/example/pkg1",
					newTestFuncRule("github.com/example/pkg1", "github.com/example/pkg1"),
				),
				newTestRuleSet(
					"github.com/example/pkg2",
					newTestFuncRule("github.com/example/pkg2", "github.com/example/pkg2"),
				),
			},
			goldenFile: "multiple_rule_sets.otel.runtime.go.golden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			sp := newTestSetupPhase()

			err := sp.addDeps(tt.matched, tmpDir)
			require.NoError(t, err)

			runtimeFilePath := filepath.Join(tmpDir, OtelRuntimeFile)

			if tt.goldenFile == "" {
				assert.NoFileExists(t, runtimeFilePath)
				return
			}

			assert.FileExists(t, runtimeFilePath)
			actual, err := os.ReadFile(runtimeFilePath)
			require.NoError(t, err)

			golden.Assert(t, string(actual), tt.goldenFile)
		})
	}
}

func TestAddDeps_FileWriteError(t *testing.T) {
	matched := []*rule.InstRuleSet{
		newTestRuleSet(
			"github.com/example/pkg",
			newTestFuncRule("github.com/example/pkg", "github.com/example/pkg"),
		),
	}

	// Use a non-existent parent directory to cause write error
	invalidPath := filepath.Join(t.TempDir(), "nonexistent", "subdir")
	sp := newTestSetupPhase()

	err := sp.addDeps(matched, invalidPath)
	assert.Error(t, err)
}

// Helper functions for constructing test data

func newTestSetupPhase() *SetupPhase {
	return &SetupPhase{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func newTestFuncRule(path, target string) *rule.InstFuncRule {
	return &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Target: target,
		},
		Path: path,
	}
}

func newTestRuleSet(modulePath string, funcRules ...*rule.InstFuncRule) *rule.InstRuleSet {
	rs := rule.NewInstRuleSet(modulePath)
	fakeFilePath := filepath.Join(os.TempDir(), "file.go")
	for _, fr := range funcRules {
		rs.AddFuncRule(fakeFilePath, fr)
	}
	return rs
}
