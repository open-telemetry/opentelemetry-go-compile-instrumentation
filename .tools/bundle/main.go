// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// bundle creates a reproducible .tar.gz archive from the given directories,
// excluding *.log files. It replaces platform-dependent tar invocations to
// produce consistent archives across platforms.
// More information: https://reproducible-builds.org/docs/archives/
package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// epoch is the fixed timestamp written to every tar header to ensure
// reproducible archives.
var epoch = time.Unix(0, 0).UTC()

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <output.tar.gz> <dir1> [dir2] ...\n", os.Args[0])
		os.Exit(1)
	}

	outPath := os.Args[1]
	dirs := os.Args[2:]

	// sort for deterministic entries
	slices.Sort(dirs)

	if err := archive(outPath, dirs); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func archive(outPath string, roots []string) (err error) {
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create output file %s: %w", outPath, err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer func() {
		if closeErr := gz.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close gzip writer: %w", closeErr)
		}
	}()

	tw := tar.NewWriter(gz)
	defer func() {
		if closeErr := tw.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close tar writer: %w", closeErr)
		}
	}()

	for _, root := range roots {
		cleanRoot := filepath.Clean(root)

		info, err := os.Lstat(cleanRoot)
		if err != nil {
			return fmt.Errorf("stat %s: %w", cleanRoot, err)
		}

		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file type: %s", cleanRoot)
		}

		base := filepath.Base(cleanRoot)
		if info.IsDir() {
			if err := addDir(tw, cleanRoot, base); err != nil {
				return err
			}
		} else {
			if err := addFile(tw, cleanRoot, base, info); err != nil {
				return err
			}
		}
	}

	return nil
}

// addDir walks dirPath in lexical order (as guaranteed by filepath.WalkDir)
// and adds every entry to the tar archive under nameInArchive.
func addDir(tw *tar.Writer, dirPath, nameInArchive string) error {
	return filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", path, err)
		}

		rel, err := filepath.Rel(dirPath, path)
		if err != nil {
			return fmt.Errorf("compute relative path for %s: %w", path, err)
		}

		entryName := filepath.Join(nameInArchive, rel)

		if shouldExclude(entryName) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}

		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file type: %s", path)
		}

		if d.IsDir() {
			// Ignore empty directories, similar to git's behavior
			empty, err := isEffectivelyEmpty(path)
			if err != nil {
				return err
			}
			if empty {
				return nil
			}

			return addDirEntry(tw, path, entryName, info)
		}

		return addFile(tw, path, entryName, info)
	})
}

// shouldExclude reports whether an archive entry should be omitted.
func shouldExclude(name string) bool {
	return strings.HasSuffix(name, ".log")
}

// isEffectivelyEmpty reports whether a directory is effectively empty,
// i.e., it contains no non-excluded files or non-empty subdirectories.
func isEffectivelyEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())

		if entry.IsDir() {
			empty, err := isEffectivelyEmpty(path)
			if err != nil {
				return false, err
			}
			if !empty {
				return false, nil
			}
			continue
		}

		if !shouldExclude(entry.Name()) {
			return false, nil
		}
	}

	return true, nil
}

// addDirEntry writes a tar header for a directory entry.
func addDirEntry(tw *tar.Writer, path, name string, info os.FileInfo) error {
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("tar header for %s: %w", path, err)
	}
	hdr.Name = filepath.ToSlash(name) + "/"
	normalizeHeader(hdr)

	return tw.WriteHeader(hdr)
}

// addFile writes a tar header and file contents for a single regular file.
func addFile(tw *tar.Writer, path, name string, info os.FileInfo) error {
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("tar header for %s: %w", path, err)
	}
	hdr.Name = filepath.ToSlash(name)
	normalizeHeader(hdr)

	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write tar header for %s: %w", path, err)
	}

	return copyFileContents(tw, path)
}

// normalizeHeader rewrites tar header metadata so archive output is
// reproducible across machines and runs.
func normalizeHeader(hdr *tar.Header) {
	hdr.ModTime = epoch
	hdr.AccessTime = epoch
	hdr.ChangeTime = epoch

	hdr.Uid = 0
	hdr.Gid = 0
	hdr.Uname = ""
	hdr.Gname = ""

	// normalize permissions to 0644/0755, similar to what git does
	if hdr.Typeflag == tar.TypeDir {
		hdr.Mode = 0o755
	} else {
		if hdr.Mode&0o111 != 0 {
			hdr.Mode = 0o755
		} else {
			hdr.Mode = 0o644
		}
	}

	hdr.Format = tar.FormatPAX
}

// copyFileContents copies the contents of path into tw.
func copyFileContents(tw io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	if _, err = io.Copy(tw, f); err != nil {
		return fmt.Errorf("copy contents of %s: %w", path, err)
	}
	return nil
}
