// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package util

import (
	"os"
	"syscall"
)

// hasOneLink reports whether the file at path has exactly one hard link. It
// does not follow a symbolic link. It returns false when it cannot tell.
func hasOneLink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Nlink == 1
}
