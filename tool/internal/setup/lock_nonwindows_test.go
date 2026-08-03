// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package setup

import (
	"io/fs"
	"os"
	"testing"

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
	path := buildLockPath()

	// Handle A locks original file
	lockA := flock.New(path)
	acquiredA, err := lockA.TryLock()
	require.NoError(t, err)
	require.True(t, acquiredA)

	// Remove original file on disk to simulate deletion race (supported on POSIX)
	require.NoError(t, os.Remove(path))

	// Handle B creates and locks a new file at the same path
	lockB := flock.New(path)
	acquiredB, err := lockB.TryLock()
	require.NoError(t, err)
	require.True(t, acquiredB)

	// lockA is now stale because path points to lockB's inode
	current, err := lockFileIsCurrent(path, lockA)
	require.NoError(t, err)
	assert.False(t, current, "stale lock handle must not be current")

	_ = lockA.Unlock()
	_ = lockA.Close()

	_ = lockB.Unlock()
	_ = lockB.Close()
}
