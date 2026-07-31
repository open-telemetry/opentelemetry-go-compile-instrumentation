// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestStateManagerDiscard(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	s := NewStateManager()

	// Track a non-existent path so Commit writes a manifest, then Discard must
	// remove the manifest and the snapshot directory.
	require.NoError(t, s.Track(util.GetBuildTemp("ghost.txt")))
	require.FileExists(t, util.GetBuildTemp(stateFileName))

	require.NoError(t, s.Discard())

	_, err := os.Stat(util.GetBuildTemp(stateFileName))
	assert.True(t, os.IsNotExist(err), "manifest must be removed")
}

func TestStateManagerDiscardEmpty(t *testing.T) {
	// Discarding an empty state manager is a no-op.
	require.NoError(t, NewStateManager().Discard())
}

func TestGenerateRuntimePerPackageSkipsPackagesWithoutFiles(t *testing.T) {
	sp := newTestSetupPhase()

	// A package with no Go files has an empty package directory and must be
	// skipped without error.
	pkgs := []*packages.Package{{PkgPath: "example.com/empty"}}
	err := sp.generateRuntimePerPackage(context.Background(), pkgs, []*rule.InstRuleSet{})
	require.NoError(t, err)
}
