// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

// lockTestDir sandboxes a lock test: buildLockPath resolves through
// OTELC_WORK_DIR before the working directory, so both must point at the
// temp dir or an ambient env var would leak the lock outside the sandbox.
func lockTestDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)
}

func TestAcquireBuildLockExcludes(t *testing.T) {
	lockTestDir(t)

	release1, err := AcquireBuildLock(t.Context())
	require.NoError(t, err)
	locked := util.PathExists(buildLockPath())

	// A second acquisition must not proceed while the first is held. The
	// goroutine never touches t after the test can have completed: its
	// error goes over a channel, and its context is canceled on cleanup.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var second atomic.Bool
	done := make(chan error, 1)
	go func() {
		release2, err2 := AcquireBuildLock(ctx)
		if err2 == nil {
			second.Store(true)
			release2()
		}
		done <- err2
	}()

	time.Sleep(3 * buildLockRetryInterval)
	waited := !second.Load()

	release1()
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("second invocation never acquired the lock after release")
	}

	require.NoError(t, err)
	assert.True(t, locked, "lock file must exist while held")
	assert.True(t, waited, "second invocation must wait for the lock holder")
	assert.True(t, second.Load())
}

func TestAcquireBuildLockCancellable(t *testing.T) {
	lockTestDir(t)

	release, err := AcquireBuildLock(t.Context())
	require.NoError(t, err)
	defer release()

	// A waiting acquisition must give up when its context is canceled —
	// this is what Ctrl-C during the wait resolves to.
	ctx, cancel := context.WithTimeout(context.Background(), 2*buildLockRetryInterval)
	defer cancel()
	_, err = AcquireBuildLock(ctx)
	require.Error(t, err)
}

func TestAcquireBuildLockRemovesFileOnRelease(t *testing.T) {
	lockTestDir(t)

	release, err := AcquireBuildLock(t.Context())
	require.NoError(t, err)
	lockedWhileHeld := util.PathExists(buildLockPath())

	// Uncontended release must leave nothing behind on any OS: POSIX
	// removes while holding, Windows removes right after the handle closes.
	release()
	assert.True(t, lockedWhileHeld, "lock file must exist while held")
	assert.False(t, util.PathExists(buildLockPath()), "lock file must be removed on release")
}

func TestAcquireBuildLockLeftoverFileDoesNotBlock(t *testing.T) {
	lockTestDir(t)

	// Simulate a crashed previous build: the file exists, but no process
	// holds a lock on it — the OS dropped the lock with the dead process.
	require.NoError(t, os.WriteFile(buildLockPath(), []byte("stale"), 0o644))

	// Acquisition must succeed promptly instead of waiting: give it a
	// deadline far shorter than anything a real contender would produce.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	release, err := AcquireBuildLock(ctx)
	require.NoError(t, err, "a leftover lock file must not block acquisition")

	release()
	assert.False(t, util.PathExists(buildLockPath()), "leftover file must be cleaned up by release")
}

func TestWithBuildLockReentrant(t *testing.T) {
	lockTestDir(t)

	// A nested withBuildLock must reuse the surrounding lock through the
	// context marker instead of re-acquiring — flock blocks a second
	// handle even within one process, so a re-acquisition attempt here
	// would poll until the deadline instead of completing instantly.
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	var nestedRan bool
	err := withBuildLock(ctx, func(ctx context.Context) error {
		return withBuildLock(ctx, func(context.Context) error {
			nestedRan = true
			return nil
		})
	})
	require.NoError(t, err)
	assert.True(t, nestedRan)
	assert.False(t, util.PathExists(buildLockPath()), "outermost release must clean up the lock file")
}

func TestAcquireBuildLockMissingWorkDirIsNoop(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	missing := tmp + "/does-not-exist/nested"
	t.Setenv(util.EnvOtelcWorkDir, missing)

	// A work dir that was never created has no module state to protect:
	// acquisition must not invent directories just to place a lock file —
	// `otelc cleanup` on a never-set-up tree stays a true no-op.
	release, err := AcquireBuildLock(t.Context())
	require.NoError(t, err)
	release()
	assert.False(t, util.PathExists(missing), "no-op acquisition must not create the work dir")
}
