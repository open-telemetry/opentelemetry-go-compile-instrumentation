// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package session holds the state otelc processes of one session share: a
// memo of resolved export files, and the slots that bound how many heavy
// package loads run at once. A session is owned by the outermost otelc build
// and inherited by every otelc process under it (see util.EnvOtelcSessionDir).
// It lasts one outer build, during which sources do not change, the same
// assumption go itself makes for a build.
package session

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

	"go.opentelemetry.io/otelc/tool/ex"
)

// lockRetryInterval is how often a waiting process re-attempts a session
// lock. The wait itself is unbounded: it ends when the holder finishes or
// the caller cancels.
const lockRetryInterval = 50 * time.Millisecond

// tryLock makes one attempt to take lock. Session lock files are never
// removed while the session lives, so a handle stays valid across attempts:
// callers keep one *flock.Flock per file for the whole wait instead of
// opening the file again on every poll. It reports contention as "not
// acquired" so the caller polls again, and returns any other failure, such
// as an unwritable path, as an error.
func tryLock(lock *flock.Flock) (bool, error) {
	acquired, err := lock.TryLock()
	if err != nil {
		if isContention(err) {
			return false, nil
		}
		return false, ex.Wrapf(err, "taking session lock %s", lock.Path())
	}
	return acquired, nil
}

// releaseLock unlocks lock and closes its handle.
func releaseLock(lock *flock.Flock) func() {
	return func() {
		_ = lock.Unlock()
		_ = lock.Close()
	}
}

// lockPath takes the lock file at path, waiting without a deadline until it
// is free or ctx is done. onWait runs once, the first time the lock is busy.
func lockPath(ctx context.Context, path string, onWait func()) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, ex.Wrapf(err, "creating lock directory for %s", path)
	}
	lock := flock.New(path)
	ok, err := tryLock(lock)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	if !ok {
		if onWait != nil {
			onWait()
		}
		ticker := time.NewTicker(lockRetryInterval)
		defer ticker.Stop()
		for !ok {
			select {
			case <-ctx.Done():
				_ = lock.Close()
				return nil, ex.Wrapf(ctx.Err(), "waiting for session lock %s", path)
			case <-ticker.C:
				if ok, err = tryLock(lock); err != nil {
					_ = lock.Close()
					return nil, err
				}
			}
		}
	}
	return releaseLock(lock), nil
}
