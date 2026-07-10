// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

// buildLockRetryInterval is how often a waiting invocation re-attempts the
// lock. The wait itself is unbounded: it ends when the holder finishes or
// the caller cancels (Ctrl-C), and a log line makes the waiting visible.
const buildLockRetryInterval = 200 * time.Millisecond

// buildLockPath returns the lock file path. The lock deliberately lives
// next to .otelc-build, not inside it: cleanup removes the whole build dir
// while holding the lock, and Windows cannot delete a file the process
// itself still holds open.
//
// The work dir is canonicalized through EvalSymlinks so that two spellings
// of the same directory (for example /tmp/app and /private/tmp/app on
// macOS) contend on one lock file instead of locking past each other.
//
// The file exists only while an invocation runs: release removes it
// best-effort (see releaseFunc), and a file left behind by a killed
// process carries no lock — the OS drops advisory locks when their holder
// dies — so a leftover can never block anyone and is silently reused.
//
// Known limitation: the lock is scoped to one module's work dir. In a
// go.work workspace, invocations for different member modules hold
// different locks yet share the workspace-level go.work.sum; serializing
// that file needs a workspace-scoped lock and is out of scope here.
func buildLockPath() string {
	workDir := util.GetOtelcWorkDir()
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = resolved
	}
	return filepath.Join(workDir, util.BuildLockFile)
}

// buildLockHeldKey marks a context whose call chain already holds the
// build lock, so nested entry points do not re-acquire it.
type buildLockHeldKey struct{}

func contextWithBuildLockHeld(ctx context.Context) context.Context {
	return context.WithValue(ctx, buildLockHeldKey{}, true)
}

func buildLockHeld(ctx context.Context) bool {
	held, _ := ctx.Value(buildLockHeldKey{}).(bool)
	return held
}

// withBuildLock runs fn under the build lock, marking the context so that
// nested calls into other locked entry points (GoBuild -> Setup -> AutoPin
// -> Pin, or GoBuild's deferred Cleanup) skip re-acquisition instead of
// deadlocking against their own process — flock blocks a second handle
// even within one process.
//
// fn must be called with the context withBuildLock passes to it. Passing a
// fresh or detached context (context.Background, errgroup with a new root,
// ...) to a nested entry point discards the held marker and turns the
// nested call into an indefinite hang.
func withBuildLock(ctx context.Context, fn func(context.Context) error) error {
	if buildLockHeld(ctx) {
		return fn(ctx)
	}
	release, err := AcquireBuildLock(ctx)
	if err != nil {
		return err
	}
	defer release()
	return fn(contextWithBuildLockHeld(ctx))
}

// AcquireBuildLock serializes otelc invocations that mutate the module —
// setup, build, cleanup, and pin all rewrite go.mod/go.sum, and all of
// them except pin's validate-only path also share .otelc-build/ (pin's
// first-run path extracts the bundle into it). Without the lock, a second
// concurrent build snapshots the first build's already-mutated go.mod as
// its "original" and the restore step then bakes the replace directives in
// permanently.
//
// The lock is an OS advisory file lock, so it is released automatically if
// the process dies, however it dies. Waiting can therefore only ever mean
// another otelc invocation is alive and running right now.
//
// A work dir that does not exist (or is not a directory) has no module
// state to protect, so acquisition degrades to a no-op instead of creating
// directories as a side effect — `otelc cleanup` on a never-set-up tree
// must stay a best-effort no-op.
//
// Every acquisition attempt opens the lock path afresh and verifies, after
// locking, that the path still names the locked file (lockFileIsCurrent).
// Both halves matter: the holder unlinks the file on release, so retrying
// on one long-lived handle could lock a deleted inode while a newer
// invocation locks a freshly created file — two winners, and exactly the
// corruption this lock exists to prevent.
//
// The returned release function must be called (deferred) by the caller.
func AcquireBuildLock(ctx context.Context) (func(), error) {
	logger := util.LoggerFromContext(ctx)
	path := buildLockPath()

	parent, statErr := os.Stat(filepath.Dir(path))
	if statErr != nil || !parent.IsDir() {
		logger.DebugContext(ctx,
			"work dir does not exist; nothing to lock", "path", path)
		return func() {}, nil
	}

	lock, acquired, leftover, err := tryAcquire(path)
	if err != nil {
		return nil, err
	}
	if acquired {
		if leftover {
			logger.DebugContext(ctx,
				"found an existing lock file that no process holds (left by a crashed or finished run); reusing it",
				"path", path)
		}
		return releaseFunc(ctx, lock), nil
	}

	fmt.Fprintf(os.Stderr,
		"otelc: another invocation holds the build lock; waiting for it to finish (lock: %s)\n",
		path,
	)

	ticker := time.NewTicker(buildLockRetryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ex.Wrapf(ctx.Err(), "waiting for build lock %s", path)
		case <-ticker.C:
			lock, acquired, _, err = tryAcquire(path)
			if err != nil {
				return nil, err
			}
			if acquired {
				return releaseFunc(ctx, lock), nil
			}
		}
	}
}

// tryAcquire makes one acquisition attempt on a freshly opened handle and
// reports whether the lock was acquired and whether the lock file already
// existed beforehand, in that order. A handle that failed to acquire — or
// that locked a file the path no longer names — is closed before
// returning, so pollers never pin a stale descriptor (which, on Windows,
// would also block the holder's removal).
//
// A transient open failure (Windows sharing violation, see
// isTransientLockFileError) counts as "not acquired" rather than an
// error: it means the file is momentarily busy — typically the holder's
// release deleting it — and the next tick will see a settled state.
//
//nolint:revive // if we add named returns then nonamedreturns will complain
func tryAcquire(path string) (*flock.Flock, bool, bool, error) {
	leftover := util.PathExists(path)
	lock := flock.New(path)
	acquired, err := lock.TryLock()
	if err != nil {
		_ = lock.Close()
		if isTransientLockFileError(err) {
			return nil, false, leftover, nil
		}
		return nil, false, leftover, ex.Wrapf(err, "acquiring build lock %s", path)
	}
	if !acquired {
		_ = lock.Close()
		return nil, false, leftover, nil
	}
	current, err := lockFileIsCurrent(path, lock)
	if err != nil {
		_ = lock.Unlock()
		return nil, false, leftover, err
	}
	if !current {
		_ = lock.Unlock()
		return nil, false, leftover, nil
	}
	return lock, true, leftover, nil
}

// lockFileIsCurrent reports whether the locked handle still corresponds to
// the file at path. A releasing holder unlinks the file just before
// unlocking, so a raced TryLock can succeed on an already-unlinked inode;
// treating that as a win would let this invocation and one locking a
// freshly created file both proceed.
//
// A missing path is the expected shape of that race and means "retry", as
// does a transient stat failure while the holder tears the file down
// (Windows sharing violation, see isTransientLockFileError). Any other
// failure is surfaced: swallowing it would leave the caller polling
// forever while claiming another invocation holds the lock.
func lockFileIsCurrent(path string, lock *flock.Flock) (bool, error) {
	held, err := lock.Stat()
	if err != nil {
		return false, ex.Wrapf(err, "statting held build lock handle")
	}
	current, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || isTransientLockFileError(err) {
			return false, nil
		}
		return false, ex.Wrapf(err, "statting build lock path %s", path)
	}
	return os.SameFile(held, current), nil
}

// releaseFunc returns the closure that releases the lock and removes the
// lock file, in an order that is safe on every supported OS:
//
//  1. remove while still holding — on POSIX this succeeds, and any waiter
//     re-opens the path fresh (see AcquireBuildLock); on Windows it fails
//     because our own handle is still open, which is expected;
//  2. unlock, which also closes the handle;
//  3. if step 1 failed, remove again — on Windows this succeeds only when
//     no other process has the file open, which is precisely when removal
//     is safe.
//
// The remove is the effective end of the critical section, not the unlock:
// callers must have finished every mutation before release runs (true
// today — every entry point defers release around its whole body), so a
// waiter that locks a freshly created file the instant after the remove is
// a correct handoff, not a race.
//
// Removal is best-effort: failures are logged at debug level and never
// propagated, because a lingering lock file is harmless — it carries no
// lock once its holder has exited.
func releaseFunc(ctx context.Context, lock *flock.Flock) func() {
	return func() {
		logger := util.LoggerFromContext(ctx)
		path := lock.Path()
		removed := os.Remove(path) == nil
		if err := lock.Unlock(); err != nil {
			logger.DebugContext(ctx, "unlocking build lock failed", "path", path, "error", err)
		}
		if !removed {
			removed = os.Remove(path) == nil
		}
		if removed {
			logger.DebugContext(ctx, "removed build lock file", "path", path)
		} else {
			logger.DebugContext(ctx,
				"build lock file left in place; another invocation has it open and will clean it up",
				"path", path)
		}
	}
}
