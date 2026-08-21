// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckTreeExactMatchPasses(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/foo/foo.go", "package foo\n")
	writeFile(t, root, "pkg/foo/foo_test.go", "package foo\n")

	violations, err := checkTree(root, nil)
	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestCheckTreeOrphanFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/foo/bar_test.go", "package foo\n")

	violations, err := checkTree(root, nil)
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.Equal(t, "pkg/foo/bar_test.go", violations[0].path)
	assert.Equal(t, "pkg/foo/bar.go", violations[0].expectedSource)
}

func TestCheckTreeAllowlistedPasses(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/foo/bar_test.go", "package foo\n")

	violations, err := checkTree(root, map[string]string{"pkg/foo/bar_test.go": "documented exception"})
	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestCheckTreeExemptScenarioDirPasses(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "test/integration/orphan_test.go", "package integration\n")

	violations, err := checkTree(root, nil)
	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestCheckTreeTestdataIgnored(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/foo/testdata/golden/orphan_test.go", "package golden\n")

	violations, err := checkTree(root, nil)
	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestCheckTreeDemoAndDotDirsIgnored(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "demo/app/basic/orphan_test.go", "package main\n")
	writeFile(t, root, ".tools/orphan_test.go", "package tools\n")

	violations, err := checkTree(root, nil)
	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestCheckTreeReportsAllViolationsSorted(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pkg/foo/zebra_test.go", "package foo\n")
	writeFile(t, root, "pkg/foo/apple_test.go", "package foo\n")

	violations, err := checkTree(root, nil)
	require.NoError(t, err)
	require.Len(t, violations, 2)
	assert.Equal(t, "pkg/foo/apple_test.go", violations[0].path)
	assert.Equal(t, "pkg/foo/zebra_test.go", violations[1].path)
}

func writeFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
