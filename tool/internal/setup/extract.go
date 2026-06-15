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

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/data"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
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

func extract(tarReader *tar.Reader, header *tar.Header, targetPath string) error {
	fileInfo := header.FileInfo()
	switch header.Typeflag {
	case tar.TypeDir:
		err := os.MkdirAll(targetPath, fileInfo.Mode())
		if err != nil {
			return ex.Wrap(err)
		}

	case tar.TypeReg:
		file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_RDWR,
			fileInfo.Mode())
		if err != nil {
			return ex.Wrap(err)
		}

		_, err = io.CopyN(file, tarReader, header.Size)
		if err != nil {
			return ex.Wrap(err)
		}
		err = file.Close()
		if err != nil {
			return ex.Wrap(err)
		}
	default:
		return ex.Newf("unsupported file type: %c in %s",
			header.Typeflag, header.Name)
	}
	return nil
}

func extractGZip(bundleReader io.Reader, targetDir string) error {
	err0 := os.MkdirAll(targetDir, 0o755)
	if err0 != nil {
		return ex.Wrap(err0)
	}

	gzReader, err0 := gzip.NewReader(bundleReader)
	if err0 != nil {
		return ex.Wrap(err0)
	}
	defer func() {
		err0 = gzReader.Close()
		if err0 != nil {
			ex.Fatal(err0)
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

		// Ensure the resolved path is within the target directory
		targetPath := filepath.Join(targetDir, cleanName)
		resolvedPath, err := filepath.EvalSymlinks(targetPath)
		if err != nil {
			// If symlink evaluation fails, use the original path
			resolvedPath = targetPath
		}

		// Check if the resolved path is within the target directory
		relPath, err := filepath.Rel(targetDir, resolvedPath)
		if err != nil || strings.HasPrefix(relPath, "..") ||
			filepath.IsAbs(relPath) {
			continue // Skip files that would be extracted outside target dir
		}
		err = extract(tarReader, header, filepath.Join(targetDir, relPath))
		if err != nil {
			return err
		}
	}

	return nil
}

func extract() error {
	// Extract the instrumentation code to the build temp directory
	// for future instrumentation phase
	return extractGZip(data.GetBundleReader(), util.GetBuildTempDir())
}
