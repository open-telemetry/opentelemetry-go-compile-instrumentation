// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package setup

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isTransientLockFileError reports whether err is a Windows sharing
// violation (ERROR_SHARING_VIOLATION). Opening or statting the lock file
// can collide with a handle that briefly excludes new opens: the holder's
// in-flight DeleteFile during release, or an antivirus/indexer scan. The
// condition clears as soon as the offending handle closes, so an
// acquisition attempt treats it as "the file is busy, try again" rather
// than as an error.
//
// ERROR_ACCESS_DENIED is deliberately not treated as transient: it can
// mean a delete-pending file on filesystems without POSIX delete
// semantics, but it is also how real permission problems surface, and
// those must fail loudly instead of retrying forever.
func isTransientLockFileError(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
