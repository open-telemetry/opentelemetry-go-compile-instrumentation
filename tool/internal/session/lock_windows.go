// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package session

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isContention reports whether err from a lock attempt means another handle
// has the file open or locked right now. Session lock files are never deleted,
// so only sharing and lock violations clear on their own.
func isContention(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
