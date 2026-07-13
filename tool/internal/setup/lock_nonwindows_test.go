// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package setup

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsTransientLockFileError(t *testing.T) {
	// POSIX has no sharing violations: nothing is transient, every open or
	// stat failure other than fs.ErrNotExist stays fatal.
	require.False(t, isTransientLockFileError(fs.ErrPermission))
	require.False(t, isTransientLockFileError(nil))
}
