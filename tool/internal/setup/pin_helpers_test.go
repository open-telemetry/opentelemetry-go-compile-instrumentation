// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

func TestIsSetup(t *testing.T) {
	// isSetup is currently a stub that always reports false.
	assert.False(t, isSetup())
}

// newModuleDir creates a minimal Go module in a fresh temp directory and
// returns its path.
func newModuleDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/vend\n\ngo 1.25\n"),
		0o644,
	))
	return dir
}

func TestPrepareVendoredBuild(t *testing.T) {
	// With no vendored module active, prepareVendoredBuild returns the args
	// unchanged and does not force module mode.
	dir := newModuleDir(t)
	t.Setenv(util.EnvOtelcWorkDir, dir)

	args := []string{"build", "./..."}
	got, err := prepareVendoredBuild(context.Background(), util.LoggerFromContext(context.Background()), args)
	require.NoError(t, err)
	assert.Equal(t, args, got)
}

func TestFindGoSources(t *testing.T) {
	dir := t.TempDir()
	srcA := filepath.Join(dir, "a.go")
	srcB := filepath.Join(dir, "b.go")
	require.NoError(t, os.WriteFile(srcA, []byte("package p\n"), 0o644))
	require.NoError(t, os.WriteFile(srcB, []byte("package p\n"), 0o644))

	args := []string{"-p", "example.com/p", srcA, srcB}
	dep, err := findGoSources(context.Background(), args, map[string]string{})
	require.NoError(t, err)
	require.NotNil(t, dep)

	assert.Equal(t, "example.com/p", dep.ImportPath)
	require.Len(t, dep.Sources, 2)
	for _, s := range dep.Sources {
		assert.True(t, filepath.IsAbs(s), "source path must be absolute: %s", s)
	}
}
