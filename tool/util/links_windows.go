// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package util

import (
	"os"

	"golang.org/x/sys/windows"
)

// hasOneLink reports whether the file at path has exactly one hard link. It
// returns false when it cannot tell.
func hasOneLink(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return false
	}
	return info.NumberOfLinks == 1
}
