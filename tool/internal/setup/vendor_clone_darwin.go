// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package setup

import "golang.org/x/sys/unix"

// platformCloneFile makes dst a clone of src with clonefile(2). On APFS, the
// clone shares the data blocks of src until one of the two files changes
// (copy on write), so it takes almost no time and no disk space. A write to
// the clone does not change src. clonefile fails when dst exists, when src is
// a symbolic link (CLONE_NOFOLLOW), and when the file system does not support
// clones (for example HFS+) or src and dst are on different volumes.
func platformCloneFile(src, dst string) error {
	return unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW)
}
