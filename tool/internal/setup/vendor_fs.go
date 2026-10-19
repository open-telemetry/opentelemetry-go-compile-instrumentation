// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
)

// The vendor directory of the user is input that otelc does not control. A
// symbolic link in it can point outside the module, so a write through the
// link changes a file outside the module. The functions in this file never
// follow a symbolic link below a root directory, and they never write outside
// that root.

// checkInside returns an error when path is not root and not inside root.
func checkInside(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ex.Newf("the path %s is outside %s", path, root)
	}
	return nil
}

// checkNoSymlink returns an error when path is outside root, or when path or
// one of its parent directories below root is a symbolic link. The check
// stops at the first part of path that does not exist.
func checkNoSymlink(root, path string) error {
	if err := checkInside(root, path); err != nil {
		return err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ex.Wrap(err)
	}
	if rel == "." {
		return nil
	}
	current := root
	for elem := range strings.SplitSeq(rel, string(filepath.Separator)) {
		current = filepath.Join(current, elem)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			return nil
		}
		if statErr != nil {
			return ex.Wrap(statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ex.Newf("%s is a symbolic link: otelc does not follow symbolic links in vendor directories",
				current)
		}
	}
	return nil
}

// lexists reports whether path exists. A symbolic link at path exists, also
// when its target does not exist.
func lexists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// copyNewFile copies the regular file src (below srcRoot) to the new file dst
// (below dstRoot). It makes the missing parent directories of dst. It fails
// when dst exists already, also as a symbolic link, so that it never replaces
// a file and never writes through a link.
func copyNewFile(srcRoot, src, dstRoot, dst string) error {
	if err := checkNoSymlink(srcRoot, src); err != nil {
		return err
	}
	if err := checkNoSymlink(dstRoot, dst); err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return ex.Wrapf(err, "reading %s", src)
	}
	if !info.Mode().IsRegular() {
		return ex.Newf("%s is not a regular file", src)
	}
	if err = os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return ex.Wrapf(err, "creating the directory of %s", dst)
	}

	in, err := os.Open(src)
	if err != nil {
		return ex.Wrapf(err, "opening %s", src)
	}
	defer in.Close()
	// O_EXCL fails when dst exists, also when dst is a symbolic link.
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return ex.Wrapf(err, "creating %s", dst)
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return ex.Wrapf(err, "copying %s to %s", src, dst)
	}
	if err = out.Close(); err != nil {
		return ex.Wrapf(err, "closing %s", dst)
	}
	return nil
}

// removeVendoredFile removes the regular file path below root. It does not
// follow a symbolic link. A file that does not exist is not an error.
func removeVendoredFile(root, path string) error {
	if err := checkNoSymlink(root, path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return ex.Wrap(err)
	}
	if !info.Mode().IsRegular() {
		return ex.Newf("%s is not a regular file", path)
	}
	if err = os.Remove(path); err != nil {
		return ex.Wrap(err)
	}
	return nil
}
