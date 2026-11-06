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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/util"
)

// buildLockRetryInterval is how often a waiting invocation re-attempts the
// lock. The wait itself is unbounded: it ends when the holder finishes or
// the caller cancels (Ctrl-C), and a log line makes the waiting visible.
const buildLockRetryInterval = 200 * time.Millisecond

// buildLockPath returns the path of the advisory lock file of the work dir.
// The lock lives next to .otelc-build, not inside it: cleanup removes the
// directory while holding the lock, and Windows cannot delete an open file.
func buildLockPath() string {
	return lockPathIn(util.GetOtelcWorkDir())
}

// lockPathIn returns the path of the advisory lock file in dir.
// EvalSymlinks canonicalizes the path so /tmp/x and /private/tmp/x share one lock.
func lockPathIn(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return filepath.Join(dir, util.BuildLockFile)
}

// buildLockPaths returns the lock files an invocation takes, sorted so that
// invocations sharing some of them always take them in the same order.
//
// The work dir lock protects .otelc-build. The module and workspace locks
// protect go.mod, go.sum, the tool file and go.work.sum, which setup edits
// and which invocations with different work dirs share: builds started from
// different directories of one module, or with different --work-dir values.
// When the work dir is the module root, both are the same file.
//
// A module or workspace lock that a parent otelc invocation holds is
// replaced by a numbered sibling. An invocation started under the parent's
// go command, for example from a test, then does not wait for its parent,
// which waits for it, but it still excludes the other invocations started
// under the same parent. The work dir lock is never replaced: a child that
// shares the work dir of its parent would overwrite the parent's
// .otelc-build, so it keeps waiting for the parent.
func buildLockPaths(ctx context.Context) []string {
	logger := util.LoggerFromContext(ctx)
	held := inheritedBuildLocks()

	paths := []string{buildLockPath()}
	for _, dir := range moduleLockDirs(ctx) {
		base := lockPathIn(dir)
		path := base
		for i := 1; slices.Contains(held, path); i++ {
			path = base + "." + strconv.Itoa(i)
		}
		if path != base {
			logger.DebugContext(ctx,
				"a parent otelc invocation holds the build lock; taking a nested lock",
				"held", base, "path", path)
		}
		if !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths
}

// moduleLockDirs returns the directories that hold the files setup edits for
// a build started in the current directory: the main module and, in
// workspace mode, the directory of go.work. It returns nil when it cannot
// resolve them, so the invocation still takes the work dir lock.
func moduleLockDirs(ctx context.Context) []string {
	logger := util.LoggerFromContext(ctx)
	cwd, err := os.Getwd()
	if err != nil {
		logger.WarnContext(ctx, "failed to get the working directory; locking only the work dir", "error", err)
		return nil
	}
	moduleDir, workFile, err := pkgload.ModuleAndWorkFile(ctx, cwd)
	if err != nil {
		logger.WarnContext(ctx, "failed to resolve the main module; locking only the work dir", "error", err)
		return nil
	}
	var dirs []string
	if moduleDir != "" {
		dirs = append(dirs, moduleDir)
	}
	if workFile != "" {
		dirs = append(dirs, filepath.Dir(workFile))
	}
	return dirs
}

// inheritedBuildLocks returns the lock files that parent otelc invocations hold.
func inheritedBuildLocks() []string {
	return filepath.SplitList(os.Getenv(util.EnvOtelcHeldBuildLocks))
}

// heldBuildLocksEnv returns the util.EnvOtelcHeldBuildLocks entry for the
// environment of a go command run under ctx: the lock files of the parent
// invocations and the ones ctx holds.
func heldBuildLocksEnv(ctx context.Context) string {
	paths, _ := ctx.Value(buildLockHeldKey{}).([]string)
	all := append(inheritedBuildLocks(), paths...)
	return util.EnvOtelcHeldBuildLocks + "=" + strings.Join(all, string(os.PathListSeparator))
}

// buildLockHeldKey marks a context whose call chain already holds the
// build lock, so nested entry points do not re-acquire it. Its value is the
// list of lock files held.
type buildLockHeldKey struct{}

func contextWithBuildLockHeld(ctx context.Context, paths []string) context.Context {
	return context.WithValue(ctx, buildLockHeldKey{}, paths)
}

func buildLockHeld(ctx context.Context) bool {
	_, held := ctx.Value(buildLockHeldKey{}).([]string)
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
	release, held, err := acquireBuildLocks(ctx, buildLockPaths(ctx))
	if err != nil {
		return err
	}
	defer release()
	return fn(contextWithBuildLockHeld(ctx, held))
}

// acquireBuildLock serializes otelc invocations that mutate the module.
// Without it, concurrent runs race on go.mod/go.sum and .otelc-build/ and
// a second invocation can snapshot already-mutated state as its "original".
// The returned release function must be called (deferred) by the caller.
func acquireBuildLock(ctx context.Context) (func(), error) {
	release, _, err := acquireBuildLocks(ctx, buildLockPaths(ctx))
	return release, err
}

// acquireBuildLocks takes the lock files in paths, in order, and returns a
// function that releases them in reverse order, along with the paths it took.
// On error, it releases the locks it already took.
func acquireBuildLocks(ctx context.Context, paths []string) (func(), []string, error) {
	var releases []func()
	var acquired []string
	releaseAll := func() {
		for _, release := range slices.Backward(releases) {
			release()
		}
	}
	for _, path := range paths {
		release, ok, err := acquireLockFile(ctx, path)
		if err != nil {
			releaseAll()
			return nil, nil, err
		}
		if ok {
			releases = append(releases, release)
			acquired = append(acquired, path)
		}
	}
	return releaseAll, acquired, nil
}

// acquireLockFile waits until it holds the lock file at path. It reports
// false, and takes nothing, when the directory of path does not exist: there
// is nothing to protect there.
// Each attempt opens the path afresh and verifies it still names the locked
// file; the holder removes the file on release so a stale handle must be
// detected and retried rather than treated as a win.
func acquireLockFile(ctx context.Context, path string) (func(), bool, error) {
	logger := util.LoggerFromContext(ctx)

	parent, statErr := os.Stat(filepath.Dir(path))
	if statErr != nil || !parent.IsDir() {
		logger.DebugContext(ctx,
			"lock dir does not exist; nothing to lock", "path", path)
		return nil, false, nil
	}

	lock, acquired, leftover, err := tryAcquire(path)
	if err != nil {
		return nil, false, err
	}
	if acquired {
		if leftover {
			logger.DebugContext(ctx,
				"found an existing lock file that no process holds (left by a crashed or finished run); reusing it",
				"path", path)
		}
		return releaseFunc(ctx, lock), true, nil
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
			return nil, false, ex.Wrapf(ctx.Err(), "waiting for build lock %s", path)
		case <-ticker.C:
			lock, acquired, _, err = tryAcquire(path)
			if err != nil {
				return nil, false, err
			}
			if acquired {
				return releaseFunc(ctx, lock), true, nil
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
		_ = lock.Close()
		return nil, false, leftover, err
	}
	if !current {
		_ = lock.Unlock()
		_ = lock.Close()
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
