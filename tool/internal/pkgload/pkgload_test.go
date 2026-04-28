// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pkgload

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

func TestLoadPackages(t *testing.T) {
	pkgs, err := LoadPackages(t.Context(), packages.NeedName, nil, "fmt")
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.Equal(t, "fmt", pkgs[0].Name)
	assert.Equal(t, "fmt", pkgs[0].PkgPath)
}

func TestResolvePackageName(t *testing.T) {
	tests := []struct {
		importPath string
		expected   string
	}{
		{"fmt", "fmt"},
		{"encoding/json", "json"},
		{"net/http", "http"},
		{"context", "context"},
		{"io", "io"},
		{"strings", "strings"},
		{"sync", "sync"},
		{"time", "time"},
		{"github.com/dave/dst", "dst"},
		{"github.com/stretchr/testify/assert", "assert"},
	}

	for _, tt := range tests {
		t.Run(tt.importPath, func(t *testing.T) {
			result := ResolvePackageName(t.Context(), tt.importPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveExportFiles(t *testing.T) {
	ctx := t.Context()

	// Test with a standard library package
	archives, err := ResolveExportFiles(ctx, "fmt")
	require.NoError(t, err)

	// Should have fmt and its dependencies
	fmtArchive, exists := archives["fmt"]
	assert.True(t, exists, "fmt should be in the result")
	assert.NotEmpty(t, fmtArchive, "fmt archive path should not be empty")

	// fmt depends on other packages, so we should have more than one
	assert.Greater(t, len(archives), 1, "should have dependencies")

	t.Logf("Resolved %d packages for fmt", len(archives))
	t.Logf("fmt archive: %s", fmtArchive)
}

func TestResolveExportFiles_InvalidPackage(t *testing.T) {
	ctx := t.Context()

	// Test with a non-existent package
	_, err := ResolveExportFiles(ctx, "this/package/does/not/exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading package")
}

func TestResolveExportFiles_MultiplePackages(t *testing.T) {
	ctx := t.Context()

	// Test with net/http which has many dependencies
	archives, err := ResolveExportFiles(ctx, "net/http")
	require.NoError(t, err)

	// Should include net/http itself
	httpArchive, exists := archives["net/http"]
	assert.True(t, exists, "net/http should be in the result")
	assert.NotEmpty(t, httpArchive, "net/http archive path should not be empty")
	assert.FileExists(t, httpArchive, "net/http export file should exist")

	// Should include some of its dependencies
	assert.Contains(t, archives, "net")
	assert.Contains(t, archives, "fmt")
	assert.FileExists(t, archives["net"], "net export file should exist")
	assert.FileExists(t, archives["fmt"], "fmt export file should exist")

	t.Logf("Resolved %d packages for net/http", len(archives))
}

func TestResolveExportFiles_NoExportFile(t *testing.T) {
	ctx := t.Context()

	// Test with "unsafe" which has no export archive
	archives, err := ResolveExportFiles(ctx, "unsafe")
	require.Error(t, err, "unsafe package should not have an export archive")
	assert.Contains(t, err.Error(), "not found or has no export file")
	assert.Nil(t, archives)
}
