// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
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
			return ex.Newf("%s is a symbolic link: otelc does not follow symbolic links below %s",
				current, root)
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

// useClones tells linkNewFile to try a clone (copy on write) first. It is
// true only on macOS (clonefile(2) on APFS). Measured on APFS with
// BenchmarkCopyVendoredModules (19,500 files): with files of 8 KiB, clones
// and copies take the same time (about 3 s, mostly to make the files), and
// hard links take 5.5 s. With files of 64 KiB, clones take 2.9 to 3.3 s and
// copies 3.4 to 5.2 s. For one file of 1 MiB, a clone is 5 times faster than
// a copy. Thus macOS uses clones, then copies, but no hard links.
//
//nolint:gochecknoglobals // tests replace it
var useClones = runtime.GOOS == "darwin"

// cloneFile is platformCloneFile. A test replaces it to make the clone fail.
//
//nolint:gochecknoglobals // tests replace it
var cloneFile = platformCloneFile

// useHardLinks tells linkNewFile to try a hard link before a copy. It is true
// only on Linux. On macOS (APFS), a hard link takes more time than a copy of
// a small file (see useClones). On Windows, a copy is the safe default.
//
//nolint:gochecknoglobals // tests replace it
var useHardLinks = runtime.GOOS == "linux"

// osLink is os.Link. A test replaces it to make the link fail.
//
//nolint:gochecknoglobals // tests replace it
var osLink = os.Link

// linkNewFile makes the new file dst (below dstRoot) from the regular file
// src (below srcRoot). It tries these ways, in this order, and uses the
// first one that works:
//  1. A clone (copy on write), when useClones is true.
//  2. A hard link, when useHardLinks is true.
//  3. A copy with copyNewFile.
//
// A clone or a link is not possible when, for example, dst is on a different
// file system, or the file system does not support it. Like copyNewFile,
// linkNewFile never replaces a file and never follows a symbolic link.
//
// A clone is an independent file: a write to dst does not change src. With a
// hard link, dst and src share their content. Thus nothing must write to
// dst: a write through dst also changes src. A replacement by a rename (for
// example util.WriteFileAtomic) and a removal are safe.
func linkNewFile(srcRoot, src, dstRoot, dst string) error {
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
	return linkOrCopyFile(srcRoot, src, dstRoot, dst)
}

// linkOrCopyFile is linkNewFile without the checks of the paths. The caller
// must check that src is a regular file below srcRoot, that no part of src
// below srcRoot is a symbolic link, and that the parent directory of dst
// exists. The copy (when the link fails) does all the checks again.
func linkOrCopyFile(srcRoot, src, dstRoot, dst string) error {
	// A clone and os.Link fail when dst exists. Then copyNewFile fails too,
	// with a clear error. A failed clone or link makes no file at dst.
	if useClones && cloneFile(src, dst) == nil {
		return nil
	}
	if useHardLinks && osLink(src, dst) == nil {
		return nil
	}
	return copyNewFile(srcRoot, src, dstRoot, dst)
}

// vendoredDirs are the import paths of the packages and of the module roots
// of a vendor/modules.txt file.
type vendoredDirs map[string]bool

// vendoredDirsOf returns the vendored packages and module roots of m.
func vendoredDirsOf(m *modulesTxt) vendoredDirs {
	dirs := make(vendoredDirs)
	for _, mod := range m.usedModules() {
		dirs[mod.Path] = true
		for _, pkg := range mod.Packages {
			dirs[pkg] = true
		}
	}
	return dirs
}

// vendoredPackages returns the vendored packages of m.
func vendoredPackages(m *modulesTxt) map[string]bool {
	pkgs := make(map[string]bool)
	for _, mod := range m.usedModules() {
		for _, pkg := range mod.Packages {
			pkgs[pkg] = true
		}
	}
	return pkgs
}

// packageFiles returns the regular files of the vendored package importPath
// in dir, which is below root. "go mod vendor" copies the files of a package
// and the files that it embeds (//go:embed). Embedded files can be in
// subdirectories, so packageFiles also returns the files of the
// subdirectories. It does not go into a subdirectory that is in own (a
// different package, or the root of a different module): that directory has
// its own files. A directory that does not exist has no files. A symbolic
// link is an error.
func packageFiles(root, dir, importPath string, own vendoredDirs) ([]string, error) {
	if err := checkNoSymlink(root, dir); err != nil {
		return nil, err
	}
	return packageTreeFiles(dir, importPath, own)
}

// packageTreeFiles is the recursive part of packageFiles. checkNoSymlink is
// not necessary below dir: each entry is examined before it is used.
func packageTreeFiles(dir, importPath string, own vendoredDirs) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, ex.Wrapf(err, "reading %s", dir)
	}
	var files []string
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		switch {
		case e.Type()&os.ModeSymlink != 0:
			return nil, ex.Newf("%s is a symbolic link: otelc does not follow symbolic links in vendor directories",
				path)
		case e.Type().IsRegular():
			files = append(files, path)
		case e.IsDir():
			sub := importPath + "/" + e.Name()
			if own[sub] {
				continue
			}
			subFiles, subErr := packageTreeFiles(path, sub, own)
			if subErr != nil {
				return nil, subErr
			}
			files = append(files, subFiles...)
		}
	}
	return files, nil
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
