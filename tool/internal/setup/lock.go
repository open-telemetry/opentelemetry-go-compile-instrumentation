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

// buildLockPath returns the path of the advisory lock file.
// The lock lives next to .otelc-build, not inside it: cleanup removes the
// directory while holding the lock, and Windows cannot delete an open file.
// EvalSymlinks canonicalizes the path so /tmp/x and /private/tmp/x share one lock.
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

// withBuildLock runs fn under the build lock, marking the context so nested
// entry points reuse the outer lock instead of deadlocking against themselves.
// fn must receive the context withBuildLock provides — a fresh context drops
// the held marker and causes the nested call to hang indefinitely.
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

// AcquireBuildLock serializes otelc invocations that mutate the module.
// Without it, concurrent runs race on go.mod/go.sum and .otelc-build/ and
// a second invocation can snapshot already-mutated state as its "original".
// If the work dir does not exist the call is a no-op (nothing to protect).
// Each attempt opens the path afresh and verifies it still names the locked
// file; the holder removes the file on release so a stale handle must be
// detected and retried rather than treated as a win.
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

	_, _ = fmt.Fprintf(os.Stderr,
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

// tryAcquire makes one lock acquisition attempt on a fresh handle.
// Returns (lock, acquired, leftoverFileExisted, err).
// Transient OS errors (e.g. Windows sharing violation during removal) are
// treated as "not acquired" rather than hard errors.
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

// lockFileIsCurrent reports whether the locked handle still names the file
// at path. The holder removes the file on release, so a raced TryLock can
// win on an unlinked inode; a missing path or transient error means retry.
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

// releaseFunc removes the lock file and releases the handle.
// Removal order: remove while holding (POSIX: succeeds; Windows: fails because
// the handle is still open) → unlock → remove again if the first attempt failed
// (Windows: now succeeds). Removal failures are logged at debug and not propagated.
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
