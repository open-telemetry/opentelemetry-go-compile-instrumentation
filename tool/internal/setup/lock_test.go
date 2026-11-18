// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

// lockTestDir sandboxes a lock test: workDirLockPath resolves through
// OTELC_WORK_DIR before the working directory, so both must point at the
// temp dir or an ambient env var would leak the lock outside the sandbox.
func lockTestDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	clearNestedBuildEnv(t)
}

// clearNestedBuildEnv clears the environment that a parent otelc invocation
// sets, so a test run under otelc does not see the locks of that invocation.
func clearNestedBuildEnv(t *testing.T) {
	t.Helper()
	t.Setenv(util.EnvOtelcHeldBuildLocks, "")
	t.Setenv(util.EnvOtelcParentWorkDirs, "")
}

// setNestedBuildEnv sets the environment that a child otelc invocation
// started under a go command run under ctx sees.
func setNestedBuildEnv(ctx context.Context, t *testing.T) {
	t.Helper()
	for _, entry := range nestedBuildEnv(ctx) {
		key, value, _ := strings.Cut(entry, "=")
		t.Setenv(key, value)
	}
}

// lockTestModule sandboxes a lock test in a module and returns its root. The
// working directory is a package directory of the module. Each test sets the
// work dir itself.
func lockTestModule(t *testing.T) string {
	t.Helper()
	moduleDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
	pkgDir := filepath.Join(moduleDir, "cmd", "app")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	t.Chdir(pkgDir)
	t.Setenv("GOWORK", "off")
	clearNestedBuildEnv(t)
	return moduleDir
}

// assertWaitsFor checks that taking paths waits while the locks that release
// frees are held, and succeeds once release runs.
func assertWaitsFor(t *testing.T, release func(), paths []string) {
	t.Helper()
	// The goroutine never touches t after the test can have completed: its
	// error goes over a channel, and its context is canceled on cleanup.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var second atomic.Bool
	done := make(chan error, 1)
	go func() {
		release2, _, err := acquireBuildLocks(ctx, paths)
		if err == nil {
			second.Store(true)
			release2()
		}
		done <- err
	}()

	time.Sleep(3 * buildLockRetryInterval)
	waited := !second.Load()

	release()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("second invocation never acquired the lock after release")
	}
	assert.True(t, waited, "second invocation must wait for the lock holder")
}

func TestAcquireBuildLockExcludes(t *testing.T) {
	lockTestDir(t)

	release1, _, err := acquireBuildLocks(t.Context(), buildLockPaths(t.Context(), ""))
	require.NoError(t, err)
	locked := util.PathExists(workDirLockPath())

	// A second acquisition must not proceed while the first is held. The
	// goroutine never touches t after the test can have completed: its
	// error goes over a channel, and its context is canceled on cleanup.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var second atomic.Bool
	done := make(chan error, 1)
	go func() {
		release2, _, err2 := acquireBuildLocks(ctx, buildLockPaths(ctx, ""))
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

	release, _, err := acquireBuildLocks(t.Context(), buildLockPaths(t.Context(), ""))
	require.NoError(t, err)
	defer release()

	// A waiting acquisition must give up when its context is canceled —
	// this is what Ctrl-C during the wait resolves to.
	ctx, cancel := context.WithTimeout(context.Background(), 2*buildLockRetryInterval)
	defer cancel()
	_, _, err = acquireBuildLocks(ctx, buildLockPaths(ctx, ""))
	require.Error(t, err)
}

func TestAcquireBuildLockRemovesFileOnRelease(t *testing.T) {
	lockTestDir(t)

	release, _, err := acquireBuildLocks(t.Context(), buildLockPaths(t.Context(), ""))
	require.NoError(t, err)
	lockedWhileHeld := util.PathExists(workDirLockPath())

	// Uncontended release must leave nothing behind on any OS: POSIX
	// removes while holding, Windows removes right after the handle closes.
	release()
	assert.True(t, lockedWhileHeld, "lock file must exist while held")
	assert.False(t, util.PathExists(workDirLockPath()), "lock file must be removed on release")
}

func TestAcquireBuildLockLeftoverFileDoesNotBlock(t *testing.T) {
	lockTestDir(t)

	// Simulate a crashed previous build: the file exists, but no process
	// holds a lock on it — the OS dropped the lock with the dead process.
	require.NoError(t, os.WriteFile(workDirLockPath(), []byte("stale"), 0o644))

	// Acquisition must succeed promptly instead of waiting: give it a
	// deadline far shorter than anything a real contender would produce.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	release, _, err := acquireBuildLocks(ctx, buildLockPaths(ctx, ""))
	require.NoError(t, err, "a leftover lock file must not block acquisition")

	release()
	assert.False(t, util.PathExists(workDirLockPath()), "leftover file must be cleaned up by release")
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
	err := withBuildLock(ctx, "", func(ctx context.Context) error {
		return withBuildLock(ctx, "", func(context.Context) error {
			nestedRan = true
			return nil
		})
	})
	require.NoError(t, err)
	assert.True(t, nestedRan)
	assert.False(t, util.PathExists(workDirLockPath()), "outermost release must clean up the lock file")
}

func TestAcquireBuildLockMissingWorkDirIsNoop(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	missing := tmp + "/does-not-exist/nested"
	t.Setenv(util.EnvOtelcWorkDir, missing)

	// A work dir that was never created has no module state to protect:
	// acquisition must not invent directories just to place a lock file —
	// `otelc cleanup` on a never-set-up tree stays a true no-op.
	release, _, err := acquireBuildLocks(t.Context(), buildLockPaths(t.Context(), ""))
	require.NoError(t, err)
	release()
	assert.False(t, util.PathExists(missing), "no-op acquisition must not create the work dir")
}

func TestBuildLockPathsWorkDirIsModuleRoot(t *testing.T) {
	moduleDir := lockTestModule(t)
	t.Setenv(util.EnvOtelcWorkDir, moduleDir)

	// The default layout keeps a single lock file, the one in the module root.
	assert.Equal(t, []string{lockPathIn(moduleDir)}, buildLockPaths(t.Context(), ""))
}

func TestBuildLockPathsIncludesWorkspace(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.25.0\n\nuse ./m\n"), 0o644))
	moduleDir := filepath.Join(root, "m")
	require.NoError(t, os.MkdirAll(moduleDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
	t.Chdir(moduleDir)
	t.Setenv("GOWORK", "")
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	clearNestedBuildEnv(t)

	paths := buildLockPaths(t.Context(), "")
	assert.Contains(t, paths, lockPathIn(moduleDir))
	assert.Contains(t, paths, lockPathIn(root), "go.work.sum is shared by every module of the workspace")
}

func TestBuildLockPathsUsesDirFlag(t *testing.T) {
	moduleDir := lockTestModule(t)
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	// otelc go build -C <dir> edits the go.mod of the module in <dir>, not
	// the one of the current directory.
	cwd := t.TempDir()
	t.Chdir(cwd)
	rel, err := filepath.Rel(cwd, moduleDir)
	require.NoError(t, err)

	assert.NotContains(t, buildLockPaths(t.Context(), ""), lockPathIn(moduleDir))
	assert.Contains(t, buildLockPaths(t.Context(), rel), lockPathIn(moduleDir))
}

func TestAcquireBuildLockExcludesAcrossWorkDirs(t *testing.T) {
	moduleDir := lockTestModule(t)

	// Two invocations in one module with different work dirs edit the same
	// go.mod, so the second must wait for the first.
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	first := buildLockPaths(t.Context(), "")
	release, _, err := acquireBuildLocks(t.Context(), first)
	require.NoError(t, err)

	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	second := buildLockPaths(t.Context(), "")

	assert.Contains(t, first, lockPathIn(moduleDir))
	assert.Contains(t, second, lockPathIn(moduleDir))
	assertWaitsFor(t, release, second)
}

func TestAcquireBuildLockNestedDoesNotWaitForParent(t *testing.T) {
	moduleDir := lockTestModule(t)
	t.Setenv(util.EnvOtelcWorkDir, moduleDir)

	err := withBuildLock(t.Context(), "", func(ctx context.Context) error {
		// A child otelc started from a test of the parent's build sees the
		// environment of the parent's go command, and uses its own work dir.
		setNestedBuildEnv(ctx, t)
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

		paths := buildLockPaths(ctx, "")
		assert.NotContains(t, paths, lockPathIn(moduleDir))
		assert.Contains(t, paths, lockPathIn(moduleDir)+".1")

		// The parent waits for its child, so the child must not wait for the
		// parent: give it a deadline far shorter than a real wait.
		waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		release, _, err := acquireBuildLocks(waitCtx, paths)
		require.NoError(t, err, "a child invocation must not wait for the lock its parent holds")
		release()
		return nil
	})
	require.NoError(t, err)
}

func TestAcquireBuildLockNestedSharedWorkDirFails(t *testing.T) {
	moduleDir := lockTestModule(t)
	t.Setenv(util.EnvOtelcWorkDir, moduleDir)

	err := withBuildLock(t.Context(), "", func(ctx context.Context) error {
		// A child that uses the work dir of its parent would overwrite the
		// parent's .otelc-build, and the parent waits for it, so it must fail
		// instead of waiting for the parent.
		setNestedBuildEnv(ctx, t)

		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, _, err := acquireBuildLocks(waitCtx, buildLockPaths(ctx, ""))
		require.ErrorContains(t, err, "--work-dir")
		assert.NotErrorIs(t, err, context.DeadlineExceeded, "a child sharing the parent's work dir must not wait")
		return nil
	})
	require.NoError(t, err)
}

func TestAcquireBuildLockNestedWorkDirIsParentModuleRoot(t *testing.T) {
	moduleDir := lockTestModule(t)
	// The parent runs in a package directory of the module, its default
	// work dir.
	pkgDir, wdErr := os.Getwd()
	require.NoError(t, wdErr)
	t.Setenv(util.EnvOtelcWorkDir, pkgDir)

	err := withBuildLock(t.Context(), "", func(ctx context.Context) error {
		// The parent holds the lock of the module root as its module lock
		// only. A child whose work dir is the module root does not share the
		// parent's .otelc-build, so it must not wait for the parent.
		setNestedBuildEnv(ctx, t)
		t.Setenv(util.EnvOtelcWorkDir, moduleDir)

		paths := buildLockPaths(ctx, "")
		assert.Equal(t, []string{lockPathIn(moduleDir) + ".1"}, paths)

		waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		release, _, err := acquireBuildLocks(waitCtx, paths)
		require.NoError(t, err, "a child that does not share the parent's work dir must not wait for the parent")
		release()
		return nil
	})
	require.NoError(t, err)
}

func TestAcquireBuildLockNestedSiblingsExclude(t *testing.T) {
	moduleDir := lockTestModule(t)
	t.Setenv(util.EnvOtelcHeldBuildLocks, encodePathList([]string{lockPathIn(moduleDir)}))

	// Two children of the same parent edit the same go.mod, so they still
	// exclude each other.
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	release, _, err := acquireBuildLocks(t.Context(), buildLockPaths(t.Context(), ""))
	require.NoError(t, err)

	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	assertWaitsFor(t, release, buildLockPaths(t.Context(), ""))

	// A grandchild whose ancestors hold the module lock and its first
	// numbered sibling takes the next one instead of waiting for its parent.
	t.Setenv(util.EnvOtelcHeldBuildLocks, encodePathList(
		[]string{lockPathIn(moduleDir), lockPathIn(moduleDir) + ".1"}))
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	assert.Contains(t, buildLockPaths(t.Context(), ""), lockPathIn(moduleDir)+".2")
}

func TestNestedBuildEnv(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcHeldBuildLocks, `["a"]`)
	t.Setenv(util.EnvOtelcParentWorkDirs, `["w"]`)
	ctx := contextWithBuildLockHeld(t.Context(), []string{"b", "c"})

	assert.Equal(t, []string{
		util.EnvOtelcHeldBuildLocks + `=["a","b","c"]`,
		util.EnvOtelcParentWorkDirs + "=" + encodePathList([]string{"w", canonicalDir(workDir)}),
	}, nestedBuildEnv(ctx))
}

func TestNestedBuildEnvKeepsPathsWithListSeparator(t *testing.T) {
	// A module whose path contains the list separator of the OS.
	moduleDir := filepath.Join(t.TempDir(), "a"+string(os.PathListSeparator)+"b")
	require.NoError(t, os.MkdirAll(filepath.Join(moduleDir, "cmd", "app"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
	t.Chdir(filepath.Join(moduleDir, "cmd", "app"))
	t.Setenv("GOWORK", "off")
	clearNestedBuildEnv(t)
	t.Setenv(util.EnvOtelcWorkDir, moduleDir)

	err := withBuildLock(t.Context(), "", func(ctx context.Context) error {
		setNestedBuildEnv(ctx, t)
		assert.Equal(t, []string{lockPathIn(moduleDir)}, envPathList(util.EnvOtelcHeldBuildLocks))
		assert.Equal(t, []string{canonicalDir(moduleDir)}, envPathList(util.EnvOtelcParentWorkDirs))

		// A child with its own work dir sees that its parent holds the module
		// lock, so it takes a numbered lock instead of waiting for its parent.
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
		assert.Contains(t, buildLockPaths(ctx, ""), lockPathIn(moduleDir)+".1")
		return nil
	})
	require.NoError(t, err)
}

func TestEnvPathListIgnoresInvalidValue(t *testing.T) {
	t.Setenv(util.EnvOtelcHeldBuildLocks, "a"+string(os.PathListSeparator)+"b")
	assert.Nil(t, envPathList(util.EnvOtelcHeldBuildLocks))
}

func TestCleanupWaitsForModuleLock(t *testing.T) {
	moduleDir := lockTestModule(t)
	// The work dir is not the module root, so the module has a lock of its own.
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	// A build of the module with another work dir holds the module lock.
	release, _, err := acquireBuildLocks(t.Context(), []string{lockPathIn(moduleDir)})
	require.NoError(t, err)

	// Cleanup restores the go.mod of the module, so it must wait for that
	// build.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Cleanup(ctx, false) }()
	select {
	case err = <-done:
		t.Fatalf("cleanup must wait for the module lock, got %v", err)
	case <-time.After(3 * buildLockRetryInterval):
	}
	release()
	select {
	case err = <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup never got the module lock after its holder ended")
	}
}

func TestAcquireBuildLockHintsAfterLongWait(t *testing.T) {
	for _, tc := range []struct {
		name        string
		parentLocks []string
		wantHint    bool
	}{
		{name: "invocation that no parent started", wantHint: true},
		{name: "nested invocation", parentLocks: []string{"parent.lock"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lockTestDir(t)
			lock := lockPathIn(t.TempDir())
			release, _, err := acquireBuildLocksUnder(t.Context(), []string{lock}, nil)
			require.NoError(t, err)

			origAfter, origOutput := lockWaitHintAfter, lockWaitOutput
			var output bytes.Buffer
			lockWaitHintAfter, lockWaitOutput = 2*buildLockRetryInterval, &output
			t.Cleanup(func() { lockWaitHintAfter, lockWaitOutput = origAfter, origOutput })

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				release2, _, err2 := acquireBuildLocksUnder(ctx, []string{lock}, tc.parentLocks)
				if err2 == nil {
					release2()
				}
				done <- err2
			}()
			time.Sleep(5 * buildLockRetryInterval)
			release()
			require.NoError(t, <-done)

			out := output.String()
			if tc.wantHint {
				assert.Contains(t, out, "still waiting for build lock "+lock)
				assert.Contains(t, out, util.EnvOtelcHeldBuildLocks)
			} else {
				assert.NotContains(t, out, "still waiting")
			}
		})
	}
}
