// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package match_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/match"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func writeMatchSource(t *testing.T, name, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	return path
}

func TestIsTestBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sources []string
		want    bool
	}{
		{name: "production sources", sources: []string{"handler.go"}, want: false},
		{name: "in-package test file", sources: []string{"handler.go", "handler_test.go"}, want: true},
		{name: "generated testmain", sources: []string{"_testmain.go"}, want: true},
		{name: "empty", sources: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, match.IsTestBuild(tt.sources))
		})
	}
}

func TestApply_FileRuleSetsPackageName(t *testing.T) {
	src := writeMatchSource(t, "mypkg.go", "package mypkg\n")
	fileRule := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{Name: "add-file", Target: "example.com/mypkg"},
		File:         "hook.go",
		Path:         "example.com/hooks",
	}
	set := rule.NewInstRuleSet("example.com/mypkg")

	err := match.Apply(t.Context(), match.Input{
		Set:     set,
		Sources: []string{src},
		Rules:   []rule.InstRule{fileRule},
	})
	require.NoError(t, err)
	assert.Equal(t, "mypkg", set.PackageName)
	require.Len(t, set.FileRules, 1)
}

func TestApply_FuncRuleKeysBySourcePath(t *testing.T) {
	matchFile := writeMatchSource(t, "match.go", "package main\n\nfunc Handler() {}\n")
	noMatchFile := writeMatchSource(t, "nomatch.go", "package main\n\nfunc Other() {}\n")
	funcRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "hook-handler", Target: "example.com/app"},
		Func:         "Handler",
		Before:       "BeforeHandler",
		Path:         "example.com/hooks",
	}
	set := rule.NewInstRuleSet("example.com/app")

	err := match.Apply(t.Context(), match.Input{
		Set:     set,
		Sources: []string{matchFile, noMatchFile},
		Rules:   []rule.InstRule{funcRule},
	})
	require.NoError(t, err)
	require.Contains(t, set.FuncRules, matchFile)
	require.NotContains(t, set.FuncRules, noMatchFile)
	assert.Equal(t, "main", set.PackageName)
}

func TestApply_WhereFileFilter(t *testing.T) {
	matchFile := writeMatchSource(t, "match.go", "package main\n\ntype Server struct{}\n\nfunc Handler() {}\n")
	noMatchFile := writeMatchSource(t, "nomatch.go", "package main\n\nfunc Handler() {}\n")
	funcRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "test-where-file",
			Target: "example.com/svc",
			Where:  &rule.WhereDef{File: &rule.FilterDef{HasStruct: "Server"}},
		},
		Func:   "Handler",
		Before: "BeforeHandler",
		Path:   "example.com/hooks",
	}
	set := rule.NewInstRuleSet("example.com/svc")

	err := match.Apply(t.Context(), match.Input{
		Set:     set,
		Sources: []string{matchFile, noMatchFile},
		Rules:   []rule.InstRule{funcRule},
	})
	require.NoError(t, err)
	require.Contains(t, set.FuncRules, matchFile)
	require.NotContains(t, set.FuncRules, noMatchFile)
}

func TestApply_EmptySourcesKeepsFileRules(t *testing.T) {
	fileRule := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{Name: "add-file", Target: "example.com/mypkg"},
		File:         "hook.go",
		Path:         "example.com/hooks",
	}
	set := rule.NewInstRuleSet("example.com/mypkg")

	err := match.Apply(t.Context(), match.Input{
		Set:     set,
		Sources: nil,
		Rules:   []rule.InstRule{fileRule},
	})
	require.NoError(t, err)
	assert.Empty(t, set.PackageName)
	assert.False(t, set.IsEmpty())
}
