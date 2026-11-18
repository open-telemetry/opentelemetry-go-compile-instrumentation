// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package session holds the state otelc processes of one session share: the
// slots that bound how many heavy package loads run at once. A session is
// owned by the outermost otelc build and inherited by every otelc process
// under it (see util.EnvOtelcSessionDir).
package session

import (
	"context"
	"time"

	"github.com/gofrs/flock"

	"go.opentelemetry.io/otelc/tool/ex"
)

// lockRetryInterval is the first delay before a waiting process re-attempts
// a session lock. Each further attempt waits twice as long, up to
// maxLockRetryInterval. The wait itself is unbounded: it ends when the holder
// finishes or the caller cancels.
const (
	lockRetryInterval    = 50 * time.Millisecond
	maxLockRetryInterval = 800 * time.Millisecond
	lockRetryGrowth      = 2
)

// pollBackoff paces the attempts of one wait. A failed attempt opens and
// closes the lock file, as gofrs/flock does for every TryLock that does not
// take the lock, so the delay between attempts doubles: a long wait costs a
// handful of opens per file instead of twenty per second.
type pollBackoff struct {
	delay time.Duration
	// polls counts the attempts made after the first one.
	polls int
}

func newPollBackoff() *pollBackoff {
	return &pollBackoff{delay: lockRetryInterval}
}

// wait sleeps until the next attempt is due, or returns ctx's error.
func (b *pollBackoff) wait(ctx context.Context) error {
	timer := time.NewTimer(b.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	b.delay = min(lockRetryGrowth*b.delay, maxLockRetryInterval)
	b.polls++
	return nil
}

// tryLock makes one attempt to take lock. Session lock files are never
// removed while the session lives, so one *flock.Flock serves a whole wait;
// gofrs/flock still reopens the file on each attempt that fails. It reports
// contention as "not acquired" so the caller polls again, and returns any
// other failure, such as an unwritable path, as an error.
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
