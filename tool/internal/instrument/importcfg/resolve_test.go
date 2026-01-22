// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package importcfg

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePackageFiles(t *testing.T) {
	ctx := context.Background()

	// Test with a standard library package
	archives, err := ResolvePackageFiles(ctx, "fmt")
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

func TestResolvePackageFiles_InvalidPackage(t *testing.T) {
	ctx := context.Background()

	// Test with a non-existent package
	_, err := ResolvePackageFiles(ctx, "this/package/does/not/exist")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "go list failed")
}

func TestResolvePackageFiles_MultiplePackages(t *testing.T) {
	ctx := context.Background()

	// Test with net/http which has many dependencies
	archives, err := ResolvePackageFiles(ctx, "net/http")
	require.NoError(t, err)

	// Should include net/http itself
	httpArchive, exists := archives["net/http"]
	assert.True(t, exists, "net/http should be in the result")
	assert.NotEmpty(t, httpArchive, "net/http archive path should not be empty")

	// Should include some of its dependencies
	assert.Contains(t, archives, "net")
	assert.Contains(t, archives, "fmt")

	t.Logf("Resolved %d packages for net/http", len(archives))
}
