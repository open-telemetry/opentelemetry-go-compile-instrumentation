// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package util

import (
	"math"
	"syscall"
)

// IsProcessAlive reports whether a process with the given PID is currently running.
func IsProcessAlive(pid int) bool {
	if pid <= 0 || pid > math.MaxUint32 {
		return false
	}
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() {
		_ = syscall.CloseHandle(h)
	}()

	var exitCode uint32
	if exitErr := syscall.GetExitCodeProcess(h, &exitCode); exitErr != nil {
		return false
	}
	const stillActive = 259
	return exitCode == stillActive
}
