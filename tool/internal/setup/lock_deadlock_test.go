// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

// deadlockTestModules are the locks of two modules, each held by a parent
// invocation, and the functions that release them.
type deadlockTestModules struct {
	lockA, lockB       string
	releaseA, releaseB func()
}

// deadlockTestLocks sandboxes a deadlock test and takes the locks of two
// modules.
func deadlockTestLocks(t *testing.T) deadlockTestModules {
	t.Helper()
	lockTestDir(t)
	m := deadlockTestModules{lockA: lockPathIn(t.TempDir()), lockB: lockPathIn(t.TempDir())}
	var err error
	m.releaseA, _, err = acquireBuildLocksUnder(t.Context(), []string{m.lockA}, nil)
	require.NoError(t, err)
	m.releaseB, _, err = acquireBuildLocksUnder(t.Context(), []string{m.lockB}, nil)
	require.NoError(t, err)
	return m
}

// writeTestWait writes a wait file for lock that an invocation holding the
// locks in holds would write, with the start time since.
func writeTestWait(t *testing.T, lock string, since time.Time, holds ...string) string {
	t.Helper()
	path := lock + waitFileInfix + "test"
	content := fmt.Sprintf("%d\n%s\n", since.UnixNano(), strings.Join(holds, "\n"))
	require.NoError(t, os.WriteFile(path, []byte("\x00"+content), 0o644))
	return path
}

// holdTestWait keeps the wait file at path locked, as its waiter does, until
// the test ends.
func holdTestWait(t *testing.T, path string) {
	t.Helper()
	flk, acquired, _, err := tryAcquire(path)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(releaseFunc(t.Context(), flk))
}

// waitFilesIn returns the names of the wait files in dir.
func waitFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, entry := range entries {
		if strings.Contains(entry.Name(), waitFileInfix) {
			names = append(names, entry.Name())
		}
	}
	return names
}

func TestAcquireBuildLockFailsOnDeadlock(t *testing.T) {
	m := deadlockTestLocks(t)

	// A child of the holder of A waits for B.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	childA := make(chan error, 1)
	go func() {
		release, _, err := acquireBuildLocksUnder(ctx, []string{m.lockB}, []string{m.lockA})
		if err == nil {
			release()
		}
		childA <- err
	}()
	require.Eventually(t, func() bool { return len(waitsFor(t.Context(), m.lockB)) == 1 },
		5*time.Second, 10*time.Millisecond, "the child of the holder of A must write a wait file")

	// A child of the holder of B waits for A, which closes the cycle.
	waitCtx, waitCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer waitCancel()
	_, _, err := acquireBuildLocksUnder(waitCtx, []string{m.lockA}, []string{m.lockB})
	require.ErrorContains(t, err, "would deadlock")

	// The child of the holder of A keeps waiting, and gets B once the holder
	// of B ends.
	select {
	case err = <-childA:
		t.Fatalf("the wait that started first must go on, got %v", err)
	case <-time.After(3 * buildLockRetryInterval):
	}
	m.releaseB()
	select {
	case err = <-childA:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the child of the holder of A never got B")
	}
	m.releaseA()

	assert.Empty(t, waitFilesIn(t, filepath.Dir(m.lockA)), "waits must remove their wait files")
	assert.Empty(t, waitFilesIn(t, filepath.Dir(m.lockB)), "waits must remove their wait files")
}

func TestAcquireBuildLockDeadlockFailsLatestWait(t *testing.T) {
	m := deadlockTestLocks(t)
	defer m.releaseA()

	// A wait for A, by a child of the holder of B, that started after the
	// wait below. It is the one to fail, so the wait below goes on.
	holdTestWait(t, writeTestWait(t, m.lockA, time.Now().Add(time.Hour), m.lockB))

	t.Setenv(util.EnvOtelcHeldBuildLocks, m.lockA)
	assertWaitsFor(t, m.releaseB, []string{m.lockB})
}

func TestAcquireBuildLockIgnoresLeftoverWaitFile(t *testing.T) {
	m := deadlockTestLocks(t)
	defer m.releaseA()

	// A process that ended left a wait file that would close a cycle.
	leftover := writeTestWait(t, m.lockA, time.Now().Add(-time.Hour), m.lockB)

	t.Setenv(util.EnvOtelcHeldBuildLocks, m.lockA)
	assertWaitsFor(t, m.releaseB, []string{m.lockB})
	assert.NoFileExists(t, leftover, "a wait file that no process holds must be removed")
}

func TestAcquireBuildLockFindsDeadlockWhileWaiting(t *testing.T) {
	m := deadlockTestLocks(t)
	defer m.releaseA()
	defer m.releaseB()

	t.Setenv(util.EnvOtelcHeldBuildLocks, m.lockA)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		release, _, err := acquireBuildLocks(ctx, []string{m.lockB})
		if err == nil {
			release()
		}
		done <- err
	}()
	require.Eventually(t, func() bool { return len(waitsFor(t.Context(), m.lockB)) == 1 },
		5*time.Second, 10*time.Millisecond)

	// A wait that started earlier closes the cycle after the wait above
	// started, so the wait above is the latest and fails.
	holdTestWait(t, writeTestWait(t, m.lockA, time.Now().Add(-time.Hour), m.lockB))
	select {
	case err := <-done:
		require.ErrorContains(t, err, "would deadlock")
	case <-time.After(5 * time.Second):
		t.Fatal("a waiting invocation must find a cycle that closes after its wait started")
	}
}

func TestAcquireBuildLockWithoutHeldLocksWritesNoWaitFile(t *testing.T) {
	lockTestDir(t)
	lock := lockPathIn(t.TempDir())
	release, _, err := acquireBuildLocksUnder(t.Context(), []string{lock}, nil)
	require.NoError(t, err)

	// An invocation that holds no lock while it waits cannot be on a cycle.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		release2, _, err2 := acquireBuildLocksUnder(ctx, []string{lock}, nil)
		if err2 == nil {
			release2()
		}
		done <- err2
	}()
	time.Sleep(3 * buildLockRetryInterval)
	assert.Empty(t, waitFilesIn(t, filepath.Dir(lock)))
	release()
	require.NoError(t, <-done)
}
