// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package session

import (
	"errors"
	"syscall"
)

// isContention reports whether err from a lock attempt means another holder
// has the lock. flock reports contention as EWOULDBLOCK, which gofrs/flock
// normally turns into "not acquired"; any other error is permanent.
func isContention(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
