// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package setup

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestIsTransientLockFileError(t *testing.T) {
	// The shape flock returns when opening the lock file collides with the
	// holder's in-flight removal or background scanner contention.
	sharing := &fs.PathError{Op: "open", Path: "x", Err: windows.ERROR_SHARING_VIOLATION}
	require.True(t, isTransientLockFileError(sharing))

	denied := &fs.PathError{Op: "open", Path: "x", Err: windows.ERROR_ACCESS_DENIED}
	require.True(t, isTransientLockFileError(denied))

	// Generic permission problems not wrapped in win32 access denied must stay fatal.
	require.False(t, isTransientLockFileError(fs.ErrPermission))
	require.False(t, isTransientLockFileError(nil))
}
