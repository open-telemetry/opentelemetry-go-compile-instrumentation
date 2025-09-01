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
	EmbeddedInstPkgGzip = "otel-pkg.gz"
)

func extractGZip(data []byte, targetDir string) error {
	err := os.MkdirAll(targetDir, 0o755)
	if err != nil {
		return ex.Error(err)
	}

	gzReader, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		return ex.Error(err)
	}
	defer func() {
		err = gzReader.Close()
		if err != nil {
			ex.Fatal(err)
		}
	}()

	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ex.Error(err)
		}

		// Skip AppleDouble files (._filename) and other hidden files
		if strings.HasPrefix(filepath.Base(header.Name), "._") ||
			strings.HasPrefix(filepath.Base(header.Name), ".") {
			continue
		}

		// Normalize path to Unix style for consistent string operations
		cleanName := filepath.ToSlash(filepath.Clean(header.Name))
		if strings.HasPrefix(cleanName, "pkg_temp/") {
			cleanName = strings.Replace(cleanName, "pkg_temp/", "pkg/", 1)
		} else if cleanName == "pkg_temp" {
			cleanName = "pkg"
		}
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
		switch header.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(targetPath, os.FileMode(header.Mode&0o777))
			if err != nil {
				return ex.Error(err)
			}

		case tar.TypeReg:
			{
				file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR,
					os.FileMode(header.Mode&0o777))
				if err != nil {
					return ex.Error(err)
				}

				_, err = io.Copy(file, tarReader)
				if err != nil {
					return ex.Error(err)
				}
				err = file.Close()
				if err != nil {
					return ex.Error(err)
				}
			}
		default:
			return ex.Errorf(nil, "unsupported file type: %c in %s",
				header.Typeflag, header.Name)
		}
	}

	return nil
}

func (*SetupPhase) extract() error {
	// Read the instrumentation code from the embedded binary file
	bs, err := data.ReadEmbedFile(EmbeddedInstPkgGzip)
	if err != nil {
		return err
	}

	// Extract the instrumentation code to the build temp directory
	// for future instrumentation phase
	err = extractGZip(bs, util.GetBuildTempDir())
	if err != nil {
		return err
	}
	return nil
}
