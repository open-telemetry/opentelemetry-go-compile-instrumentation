// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegisterReleaseBuild_Single(t *testing.T) {
	tmpDir := t.TempDir()
	appBin := "app"
	binPath := filepath.Join(tmpDir, appBin)
	buildDir := filepath.Join(tmpDir, ".otelc-build")
	lockFile := filepath.Join(tmpDir, ".otelc-build.lock")

	require.NoError(t, os.WriteFile(binPath, []byte("fake-bin"), 0o755))
	require.NoError(t, os.Mkdir(buildDir, 0o755))
	require.NoError(t, os.WriteFile(lockFile, []byte("lock"), 0o644))

	registerBuild(tmpDir)
	require.Equal(t, 1, buildRefsCount(tmpDir))

	releaseBuild(tmpDir, appBin)
	require.Equal(t, 0, buildRefsCount(tmpDir))

	_, err := os.Stat(binPath)
	require.True(t, os.IsNotExist(err), "binary should be removed")
	_, err = os.Stat(buildDir)
	require.True(t, os.IsNotExist(err), ".otelc-build should be removed")
	_, err = os.Stat(lockFile)
	require.True(t, os.IsNotExist(err), ".otelc-build.lock should be removed")
}

func TestRegisterReleaseBuild_ConcurrentSharing(t *testing.T) {
	tmpDir := t.TempDir()
	appBin := "app"
	binPath := filepath.Join(tmpDir, appBin)
	buildDir := filepath.Join(tmpDir, ".otelc-build")
	lockFile := filepath.Join(tmpDir, ".otelc-build.lock")

	require.NoError(t, os.WriteFile(binPath, []byte("fake-bin"), 0o755))
	require.NoError(t, os.Mkdir(buildDir, 0o755))
	require.NoError(t, os.WriteFile(lockFile, []byte("lock"), 0o644))

	// Simulate 3 parallel tests sharing the same appDir
	registerBuild(tmpDir)
	registerBuild(tmpDir)
	registerBuild(tmpDir)
	require.Equal(t, 3, buildRefsCount(tmpDir))

	// First test completes; resources should NOT be deleted
	releaseBuild(tmpDir, appBin)
	require.Equal(t, 2, buildRefsCount(tmpDir))
	require.DirExists(t, buildDir, ".otelc-build must not be deleted while other tests are active")
	require.FileExists(t, binPath, "app binary must not be deleted while other tests are active")
	require.FileExists(t, lockFile, ".otelc-build.lock must not be deleted while other tests are active")

	// Second test completes; resources should still NOT be deleted
	releaseBuild(tmpDir, appBin)
	require.Equal(t, 1, buildRefsCount(tmpDir))
	require.DirExists(t, buildDir, ".otelc-build must still exist for the remaining active test")

	// Third and final test completes; now everything should be cleaned up
	releaseBuild(tmpDir, appBin)
	require.Equal(t, 0, buildRefsCount(tmpDir))

	_, err := os.Stat(binPath)
	require.True(t, os.IsNotExist(err), "binary should be removed after last reference released")
	_, err = os.Stat(buildDir)
	require.True(t, os.IsNotExist(err), ".otelc-build should be removed after last reference released")
	_, err = os.Stat(lockFile)
	require.True(t, os.IsNotExist(err), ".otelc-build.lock should be removed after last reference released")
}

func TestRegisterReleaseBuild_GoroutineSafety(t *testing.T) {
	tmpDir := t.TempDir()
	appBin := "app"

	const concurrency = 50
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			registerBuild(tmpDir)
			// Simulate test execution
			releaseBuild(tmpDir, appBin)
		}()
	}

	wg.Wait()
	require.Equal(t, 0, buildRefsCount(tmpDir), "final ref count must be 0")
}
