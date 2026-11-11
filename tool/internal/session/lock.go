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

// tryLock makes one attempt to take the lock file at path. Session lock files
// are never removed while the session lives, so a handle stays valid. An
// error opening the file counts as "not acquired": Windows reports a sharing
// violation while another process opens it, and the caller retries.
func tryLock(path string) (*flock.Flock, bool) {
	lock := flock.New(path)
	acquired, err := lock.TryLock()
	if err != nil || !acquired {
		_ = lock.Close()
		return nil, false
	}
	return lock, true
}

// lockPath takes the lock file at path, waiting without a deadline until it
// is free or ctx is done. onWait runs once, the first time the lock is busy.
func lockPath(ctx context.Context, path string, onWait func()) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, ex.Wrapf(err, "creating lock directory for %s", path)
	}
	lock, ok := tryLock(path)
	if !ok {
		if onWait != nil {
			onWait()
		}
		ticker := time.NewTicker(lockRetryInterval)
		defer ticker.Stop()
		for !ok {
			select {
			case <-ctx.Done():
				return nil, ex.Wrapf(ctx.Err(), "waiting for session lock %s", path)
			case <-ticker.C:
				lock, ok = tryLock(path)
			}
		}
	}
	return func() {
		_ = lock.Unlock()
		_ = lock.Close()
	}, nil
}
