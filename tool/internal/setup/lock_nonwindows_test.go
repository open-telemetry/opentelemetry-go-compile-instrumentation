// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package setup

import (
	"bytes"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

func TestBuildLockPathsWithoutGoEnv(t *testing.T) {
	lockTestModule(t)
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	// When go env fails, the invocation still takes the work dir lock.
	goDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(goDir, "go"), []byte("#!/bin/sh\nexit 1\n"), 0o755))
	t.Setenv("PATH", goDir)

	assert.Equal(t, []string{lockPathIn(workDir)}, buildLockPaths(t.Context(), ""))
}

func TestRegisterWaitWarnsWithoutWaitFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can create files in a read-only directory")
	}
	// The wait file goes next to the lock, so it cannot be created in a
	// read-only directory.
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	var logs bytes.Buffer
	ctx := util.ContextWithLogger(t.Context(), slog.New(slog.NewTextHandler(&logs, nil)))
	self, unregister := registerWait(ctx, lockPathIn(dir), []string{lockPathIn(t.TempDir())})
	unregister()

	assert.Empty(t, self.file)
	// Without a wait file, other invocations cannot find a deadlock with this
	// wait, so the failure must be visible at the default log level.
	assert.Contains(t, logs.String(), "level=WARN")
	assert.Contains(t, logs.String(), "failed to write a wait file")
	assert.Contains(t, logs.String(), "permission denied")
}

func TestIsTransientLockFileError(t *testing.T) {
	// POSIX has no sharing violations: nothing is transient, every open or
	// stat failure other than fs.ErrNotExist stays fatal.
	require.False(t, isTransientLockFileError(fs.ErrPermission))
	require.False(t, isTransientLockFileError(nil))
}

func TestTryAcquireStaleLockCleanup(t *testing.T) {
	lockTestDir(t)
	tmpDir := t.TempDir()
	path := workDirLockPath()

	// 1. Verify lockFileIsCurrent returns false for stale lock handles
	lockA := flock.New(path)
	acquiredA, err := lockA.TryLock()
	require.NoError(t, err)
	require.True(t, acquiredA)

	require.NoError(t, os.Remove(path))

	lockB := flock.New(path)
	acquiredB, err := lockB.TryLock()
	require.NoError(t, err)
	require.True(t, acquiredB)

	current, err := lockFileIsCurrent(path, lockA)
	require.NoError(t, err)
	assert.False(t, current, "stale lock handle must not be current")

	_ = lockA.Unlock()
	_ = lockA.Close()
	_ = lockB.Unlock()
	_ = lockB.Close()

	// 2. Directly invoke tryAcquire under unlinked file condition to exercise
	// the !current cleanup path inside tryAcquire.
	raceFile := filepath.Join(tmpDir, "racefile.lock")
	require.NoError(t, os.WriteFile(raceFile, []byte("data"), 0o644))

	go func() {
		time.Sleep(1 * time.Millisecond)
		_ = os.Remove(raceFile)
	}()

	l, acq, _, err := tryAcquire(raceFile)
	require.NoError(t, err)
	if !acq {
		assert.Nil(t, l, "tryAcquire must return nil handle on stale lock cleanup")
	} else {
		_ = l.Unlock()
		_ = l.Close()
	}
}
