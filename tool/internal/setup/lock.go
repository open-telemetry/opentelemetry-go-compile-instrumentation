// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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

// lockWaitHintAfter is how long an invocation that no parent started waits
// for a lock before it prints a hint about nested invocations. It is a
// variable so tests can shorten it.
//
//nolint:gochecknoglobals // test seam
var lockWaitHintAfter = 10 * time.Second

// lockWaitOutput receives the messages about lock waits. It is a variable so
// tests can read them.
//
//nolint:gochecknoglobals // test seam
var lockWaitOutput io.Writer = os.Stderr

// workDirLockPath returns the path of the advisory lock file of the work dir.
// The lock lives next to .otelc-build, not inside it: cleanup removes the
// directory while holding the lock, and Windows cannot delete an open file.
func workDirLockPath() string {
	return lockPathIn(util.GetOtelcWorkDir())
}

// lockPathIn returns the path of the advisory lock file in dir.
func lockPathIn(dir string) string {
	return filepath.Join(canonicalDir(dir), util.BuildLockFile)
}

// canonicalDir resolves the symlinks in dir, so /tmp/x and /private/tmp/x
// share one lock. It returns dir unchanged when dir does not exist.
func canonicalDir(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}

// buildLockPaths returns the lock files an invocation takes, sorted so that
// invocations sharing some of them always take them in the same order. dir
// is the directory of the go command, from its -C flag, or empty for the
// current directory.
//
// The work dir lock protects .otelc-build. The module and workspace locks
// protect go.mod, go.sum, the tool file and go.work.sum, which setup edits
// and which invocations with different work dirs share: builds started from
// different directories of one module, or with different --work-dir values.
// When the work dir is the module root, both are the same file.
//
// A lock that a parent otelc invocation holds is replaced by a numbered
// sibling. An invocation started under the parent's go command, for example
// from a test, then does not wait for its parent, which waits for it, but it
// still excludes the other invocations started under the same parent. A
// child's descendants take higher numbers, and the other children wait for
// that child. A child that uses the work dir of a parent keeps the work dir
// lock, and fails while the parent holds it: it would overwrite the parent's
// .otelc-build.
//
// A child does not take its locks in the same order as other invocations,
// because its parent already holds some of them. A child and an unrelated
// invocation that share a --work-dir can then wait for each other, and one of
// them fails with a deadlock error (see lock_deadlock.go).
func buildLockPaths(ctx context.Context, dir string) []string {
	held := envPathList(util.EnvOtelcHeldBuildLocks)

	workDirLock := workDirLockPath()
	if !slices.Contains(envPathList(util.EnvOtelcParentWorkDirs), canonicalDir(util.GetOtelcWorkDir())) {
		workDirLock = unheldLockPath(ctx, workDirLock, held)
	}
	paths := []string{workDirLock}
	for _, lockDir := range moduleLockDirs(ctx, dir) {
		path := unheldLockPath(ctx, lockPathIn(lockDir), held)
		if !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths
}

// unheldLockPath returns base, or the first numbered sibling of base that no
// parent otelc invocation holds.
func unheldLockPath(ctx context.Context, base string, held []string) string {
	path := base
	for i := 1; slices.Contains(held, path); i++ {
		path = base + "." + strconv.Itoa(i)
	}
	if path != base {
		util.LoggerFromContext(ctx).DebugContext(ctx,
			"a parent otelc invocation holds the build lock; taking a nested lock",
			"held", base, "path", path)
	}
	return path
}

// moduleLockDirs returns the directories that hold the files setup edits for
// a go command run in dir, or in the current directory when dir is empty:
// the main module and, in workspace mode, the directory of go.work. It
// returns nil when it cannot resolve them, so the invocation still takes the
// work dir lock.
// It locks only the main module: a workspace build that edits another
// module's go.mod shares no lock with a GOWORK=off build of that module.
func moduleLockDirs(ctx context.Context, dir string) []string {
	logger := util.LoggerFromContext(ctx)
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			logger.WarnContext(ctx, "failed to get the working directory; locking only the work dir", "error", err)
			return nil
		}
		dir = cwd
	}
	moduleDir, workFile, err := pkgload.ModuleAndWorkFile(ctx, dir)
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

// envPathList returns the paths in the environment variable key, a JSON
// array of strings. A path can contain any character, including the list
// separator of the OS. It returns nil when the variable is not set or is not
// valid.
func envPathList(key string) []string {
	value := os.Getenv(key)
	if value == "" {
		return nil
	}
	var paths []string
	if err := json.Unmarshal([]byte(value), &paths); err != nil {
		return nil
	}
	return paths
}

// encodePathList returns paths in the format that envPathList reads.
func encodePathList(paths []string) string {
	// Marshaling a slice of strings cannot fail.
	data, _ := json.Marshal(paths)
	return string(data)
}

// nestedBuildEnv returns the environment entries that tell otelc
// invocations started under a go command run under ctx which lock files
// their parents hold and which work dirs their parents use.
func nestedBuildEnv(ctx context.Context) []string {
	held, _ := ctx.Value(buildLockHeldKey{}).([]string)
	locks := append(envPathList(util.EnvOtelcHeldBuildLocks), held...)
	workDirs := append(envPathList(util.EnvOtelcParentWorkDirs), canonicalDir(util.GetOtelcWorkDir()))
	return []string{
		util.EnvOtelcHeldBuildLocks + "=" + encodePathList(locks),
		util.EnvOtelcParentWorkDirs + "=" + encodePathList(workDirs),
	}
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
// The lock serializes otelc invocations that mutate the module. Without it,
// concurrent runs race on go.mod/go.sum and .otelc-build/ and a second
// invocation can snapshot already-mutated state as its "original".
// dir is the directory of the go command, from its -C flag, or empty for
// the current directory.
// fn must receive the context withBuildLock provides — a fresh context drops
// the held marker and causes the nested call to hang indefinitely.
func withBuildLock(ctx context.Context, dir string, fn func(context.Context) error) error {
	if buildLockHeld(ctx) {
		return fn(ctx)
	}
	release, held, err := acquireBuildLocks(ctx, buildLockPaths(ctx, dir))
	if err != nil {
		return err
	}
	defer release()
	return fn(contextWithBuildLockHeld(ctx, held))
}

// acquireBuildLocks takes the lock files in paths, in order, and returns a
// function that releases them in reverse order, along with the paths it took.
// On error, it releases the locks it already took.
func acquireBuildLocks(ctx context.Context, paths []string) (func(), []string, error) {
	return acquireBuildLocksUnder(ctx, paths, envPathList(util.EnvOtelcHeldBuildLocks))
}

// acquireBuildLocksUnder is acquireBuildLocks for an invocation whose parent
// otelc invocations hold the lock files in parentLocks.
func acquireBuildLocksUnder(ctx context.Context, paths, parentLocks []string) (func(), []string, error) {
	var releases []func()
	var acquired []string
	releaseAll := func() {
		for _, release := range slices.Backward(releases) {
			release()
		}
	}
	for _, path := range paths {
		release, ok, err := acquireLockFile(ctx, path, parentLocks, acquired)
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
// is nothing to protect there. parentLocks are the locks that parent otelc
// invocations hold, and taken are the locks that the invocation already
// took.
// Each attempt opens the path afresh and verifies it still names the locked
// file; the holder removes the file on release so a stale handle must be
// detected and retried rather than treated as a win.
func acquireLockFile(ctx context.Context, path string, parentLocks, taken []string) (func(), bool, error) {
	logger := util.LoggerFromContext(ctx)

	if !isDir(filepath.Dir(path)) {
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
		removeLeftoverWaits(ctx, path)
		return releaseFunc(ctx, lock), true, nil
	}

	// A lock that a parent holds is freed only when the parent ends, and the
	// parent waits for this invocation. It is the work dir lock of a child
	// that uses the work dir of its parent.
	if slices.Contains(parentLocks, path) {
		return nil, false, ex.Newf("build lock %s is held by a parent otelc invocation that waits for this one: "+
			"an otelc command started under another otelc command, for example from a test, "+
			"must use a work dir of its own (--work-dir)", path)
	}

	_, _ = fmt.Fprintf(lockWaitOutput,
		"otelc: another invocation holds the build lock; waiting for it to finish (lock: %s)\n",
		path,
	)
	return waitForLockFile(ctx, path, parentLocks, taken)
}

// isDir reports whether path is an existing directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// waitForLockFile retries the lock file at path until it holds it. While the
// invocation or its parents hold locks, other invocations can see the wait,
// and the wait fails when it would deadlock (see lock_deadlock.go).
func waitForLockFile(ctx context.Context, path string, parentLocks, taken []string) (func(), bool, error) {
	holds := append(slices.Clone(parentLocks), taken...)
	checkDeadlock := len(holds) > 0
	// An invocation started under another otelc command that did not get its
	// environment looks like one that no parent started.
	hint := len(parentLocks) == 0
	waitStart := time.Now()
	var self waitRecord
	if checkDeadlock {
		var unregister func()
		self, unregister = registerWait(ctx, path, holds)
		defer unregister()
		if err := deadlockError(ctx, self); err != nil {
			return nil, false, err
		}
	}

	ticker := time.NewTicker(buildLockRetryInterval)
	defer ticker.Stop()
	for tick := 1; ; tick++ {
		select {
		case <-ctx.Done():
			return nil, false, ex.Wrapf(ctx.Err(), "waiting for build lock %s", path)
		case <-ticker.C:
			lock, acquired, _, err := tryAcquire(path)
			if err != nil {
				return nil, false, err
			}
			if acquired {
				removeLeftoverWaits(ctx, path)
				return releaseFunc(ctx, lock), true, nil
			}
			// A cycle can also close after the wait started, when a lock
			// changes hands.
			if checkDeadlock && tick%deadlockCheckTicks == 0 {
				if err = deadlockError(ctx, self); err != nil {
					return nil, false, err
				}
			}
			if hint && time.Since(waitStart) >= lockWaitHintAfter {
				hint = false
				_, _ = fmt.Fprintf(lockWaitOutput,
					"otelc: still waiting for build lock %s. An otelc command started under another otelc "+
						"command, for example from a test, must get the %s environment variable of that "+
						"command, or it can wait for it forever\n",
					path, util.EnvOtelcHeldBuildLocks)
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
