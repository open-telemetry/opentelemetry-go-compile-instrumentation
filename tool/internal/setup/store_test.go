// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

func TestResolveRulePaths(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"),
		0o644,
	))

	hooksDir := filepath.Join(dir, "hooks")
	require.NoError(t, os.MkdirAll(hooksDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(hooksDir, "hook.go"),
		[]byte("package hooks\n"),
		0o644,
	))

	rs := &rule.InstRuleSet{
		FuncRules: map[string][]*rule.InstFuncRule{
			"foo": {{
				Path: "example.com/test/hooks",
			}},
		},
		FileRules: []*rule.InstFileRule{{
			Path: "example.com/test/hooks",
		}},
	}

	err := resolveRulePaths(
		t.Context(),
		[]*rule.InstRuleSet{rs},
		map[string]bool{dir: true},
	)
	require.NoError(t, err)

	require.Equal(t, hooksDir, rs.AllFuncRules()[0].ResolvedPath)
	require.Equal(t, hooksDir, rs.FileRules[0].ResolvedPath)
}

func TestResolveRulePaths_NotFound(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"),
		0o644,
	))

	rs := &rule.InstRuleSet{
		FuncRules: map[string][]*rule.InstFuncRule{
			"foo": {{
				Path: "example.com/test/doesnotexist",
			}},
		},
	}

	err := resolveRulePaths(
		t.Context(),
		[]*rule.InstRuleSet{rs},
		map[string]bool{dir: true},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to resolve import path")
}
