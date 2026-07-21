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
	// holder's in-flight removal.
	sharing := &fs.PathError{Op: "open", Path: "x", Err: windows.ERROR_SHARING_VIOLATION}
	require.True(t, isTransientLockFileError(sharing))

	// Real permission problems must stay fatal.
	require.False(t, isTransientLockFileError(fs.ErrPermission))
	require.False(t, isTransientLockFileError(nil))
}
