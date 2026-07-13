// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package setup

// isTransientLockFileError reports whether err is a transient, retryable
// failure to open or stat the lock file. Only Windows exhibits these (see
// lock_windows.go): on POSIX systems, unlinking a path never invalidates
// concurrent opens of it.
func isTransientLockFileError(_ error) bool {
	return false
}
