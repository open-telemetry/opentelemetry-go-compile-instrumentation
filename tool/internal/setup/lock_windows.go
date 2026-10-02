// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package setup

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isTransientLockFileError reports whether err is a transient Windows error
// caused by concurrent lock-file operations, specifically:
//   - ERROR_SHARING_VIOLATION (32) which occurs when opening a file that is in the
//     middle of being deleted by another process, or when statting a file whose
//     delete-on-close handle hasn't fully closed yet.
//   - ERROR_ACCESS_DENIED (5) which occurs when opening a file marked for deletion
//     (delete-pending) by the releasing process before its handle teardown completes,
//     or during transient background scanner/indexer file opens.
//
// Both conditions clear as soon as the offending handle closes, so an acquisition
// attempt treats them as "the file is busy, try again" rather than as an error.
func isTransientLockFileError(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
