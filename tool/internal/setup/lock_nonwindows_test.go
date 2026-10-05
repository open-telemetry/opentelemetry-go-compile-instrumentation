// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package setup

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsTransientLockFileError(t *testing.T) {
	// POSIX has no sharing violations: nothing is transient, every open or
	// stat failure other than fs.ErrNotExist stays fatal.
	require.False(t, isTransientLockFileError(fs.ErrPermission))
	require.False(t, isTransientLockFileError(nil))
}

func TestTryAcquireStaleLockCleanup(t *testing.T) {
	lockTestDir(t)
	tmpDir := t.TempDir()
	path := buildLockPath()

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
