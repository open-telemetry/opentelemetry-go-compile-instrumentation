// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otelc/tool/data"
	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

const (
	unzippedPkgDir  = "pkg"
	unzippedInstDir = "instrumentation"

	pkgTempDir  = "pkg_temp"
	instTempDir = "instrumentation_temp"
)

func normalizePath(name string) string {
	cleanName := filepath.ToSlash(filepath.Clean(name))

	replacements := map[string]string{
		pkgTempDir:  unzippedPkgDir,
		instTempDir: unzippedInstDir,
	}

	for from, to := range replacements {
		if cleanName == from {
			return to
		}
		if strings.HasPrefix(cleanName, from+"/") {
			return to + strings.TrimPrefix(cleanName, from)
		}
	}

	return cleanName
}

// extract writes the archive entry header to targetPath. The caller makes
// sure that targetPath and its parent directories (below the extraction root)
// are not symbolic links. A regular file is written to a temporary file in
// the same directory, then renamed to targetPath: thus otelc does not write
// through a symbolic link or a hard link at targetPath.
func extract(tarReader *tar.Reader, header *tar.Header, targetPath string) error {
	fileInfo := header.FileInfo()
	switch header.Typeflag {
	case tar.TypeDir:
		err := os.MkdirAll(targetPath, fileInfo.Mode())
		if err != nil {
			return ex.Wrap(err)
		}

	case tar.TypeReg:
		dir := filepath.Dir(targetPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return ex.Wrap(err)
		}
		file, err := os.CreateTemp(dir, filepath.Base(targetPath)+".tmp-*")
		if err != nil {
			return ex.Wrap(err)
		}
		tmpPath := file.Name()
		defer func() {
			// After a successful rename, the file does not exist anymore.
			_ = os.Remove(tmpPath)
		}()

		_, err = io.CopyN(file, tarReader, header.Size)
		if err != nil {
			_ = file.Close()
			return ex.Wrap(err)
		}
		err = file.Close()
		if err != nil {
			return ex.Wrap(err)
		}
		if err = os.Chmod(tmpPath, fileInfo.Mode().Perm()); err != nil {
			return ex.Wrap(err)
		}
		if err = util.RenameReplace(tmpPath, targetPath); err != nil {
			return ex.Wrap(err)
		}
	default:
		return ex.Newf("unsupported file type: %c in %s",
			header.Typeflag, header.Name)
	}
	return nil
}

func extractGZip(bundleReader io.Reader, targetDir string) (retErr error) {
	// The target directory itself must not be a symbolic link: else all the
	// files go to the target of the link, which can be outside the project.
	// checkNoSymlink below checks only the paths below the target directory.
	if info, err := os.Lstat(targetDir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return ex.Newf("%s is a symbolic link: otelc does not extract its bundle through a symbolic link",
			targetDir)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return ex.Wrap(err)
	}

	gzReader, gzErr := gzip.NewReader(bundleReader)
	if gzErr != nil {
		return ex.Wrap(gzErr)
	}
	defer func() {
		if closeErr := gzReader.Close(); closeErr != nil {
			retErr = errors.Join(retErr, ex.Wrap(closeErr))
		}
	}()

	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ex.Wrap(err)
		}

		// Skip AppleDouble files (._filename) and other hidden files
		if strings.HasPrefix(filepath.Base(header.Name), ".") {
			continue
		}

		// Normalize path to Unix style for consistent string operations
		cleanName := normalizePath(header.Name)

		// Sanitize the file path to prevent Zip Slip vulnerability
		if cleanName == "." || cleanName == ".." ||
			strings.HasPrefix(cleanName, "..") {
			continue
		}

		// The path must be inside the target directory, and otelc does not
		// follow a symbolic link below the target directory: a link (for
		// example one that a forged state file restored) can point outside
		// it. A part of the path that does not exist yet is not a link.
		targetPath := filepath.Join(targetDir, filepath.FromSlash(cleanName))
		if checkInside(targetDir, targetPath) != nil {
			continue // Skip files that would be extracted outside target dir
		}
		if err = checkNoSymlink(targetDir, targetPath); err != nil {
			return ex.Wrapf(err, "extracting %s", header.Name)
		}
		err = extract(tarReader, header, targetPath)
		if err != nil {
			return err
		}
	}

	return nil
}

func extractOtelcBundle() error {
	// Extract the instrumentation code to the build temp directory
	// for future instrumentation phase
	return extractGZip(data.GetBundleReader(), util.GetBuildTempDir())
}
