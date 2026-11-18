// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
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
			_ = file.Close()
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

func extractGZip(bundleReader io.Reader, targetDir string) (retErr error) {
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

		// Sanitize the file path to prevent Zip Slip vulnerability.
		// filepath.IsLocal is checked directly here so CodeQL's taint
		// tracking recognizes the destination path as sanitized.
		name := filepath.FromSlash(cleanName)
		if !filepath.IsLocal(name) {
			continue // Skip files that would be extracted outside target dir
		}

		targetPath := filepath.Join(targetDir, name)
		err = extract(tarReader, header, targetPath)
		if err != nil {
			return err
		}
	}

	return nil
}

// refreshExtractedBundle replaces the pkg and instrumentation directories of
// targetDir with the contents of the bundle. extractGZip only writes files, so
// a file that an older otelc extracted and the bundle no longer ships would
// stay and be compiled together with the files that replaced it. Other
// entries of targetDir, such as the Go build cache, are left alone.
// bundleStampFile records the hash of the bundle extracted in the build temp
// directory.
const bundleStampFile = "bundle.sha256"

func refreshExtractedBundle(bundleReader io.ReadSeeker, targetDir string) error {
	hash := sha256.New()
	if _, err := io.Copy(hash, bundleReader); err != nil {
		return ex.Wrapf(err, "hashing the bundle")
	}
	if _, err := bundleReader.Seek(0, io.SeekStart); err != nil {
		return ex.Wrapf(err, "rewinding the bundle")
	}
	sum := hex.EncodeToString(hash.Sum(nil))

	// The go.mod of a build that already released the build lock points at
	// these directories while it compiles. Extract again only if the bundle
	// changed, so that a later setup does not remove them under it.
	stamp := filepath.Join(targetDir, bundleStampFile)
	if old, err := os.ReadFile(stamp); err == nil && string(old) == sum &&
		util.PathExists(filepath.Join(targetDir, unzippedPkgDir)) &&
		util.PathExists(filepath.Join(targetDir, unzippedInstDir)) {
		return nil
	}

	if err := os.Remove(stamp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ex.Wrapf(err, "removing the bundle stamp")
	}
	for _, dir := range []string{unzippedPkgDir, unzippedInstDir} {
		if err := os.RemoveAll(filepath.Join(targetDir, dir)); err != nil {
			return ex.Wrapf(err, "removing the previously extracted %s directory", dir)
		}
	}
	if err := extractGZip(bundleReader, targetDir); err != nil {
		return err
	}
	return util.WriteFileAtomic(stamp, []byte(sum))
}

func extractOtelcBundle() error {
	// Extract the instrumentation code to the build temp directory
	// for future instrumentation phase
	return refreshExtractedBundle(data.GetBundleReader(), util.GetBuildTempDir())
}
