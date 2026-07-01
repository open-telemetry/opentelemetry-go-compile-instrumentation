// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "pkg temp dir",
			in:   pkgTempDir,
			want: unzippedPkgDir,
		},
		{
			name: "pkg temp file",
			in:   pkgTempDir + "/rules.yaml",
			want: unzippedPkgDir + "/rules.yaml",
		},
		{
			name: "instrumentation temp dir",
			in:   instTempDir,
			want: unzippedInstDir,
		},
		{
			name: "instrumentation temp file",
			in:   instTempDir + "/http/client/config.yaml",
			want: unzippedInstDir + "/http/client/config.yaml",
		},
		{
			name: "normal path unchanged",
			in:   "other/file.yaml",
			want: "other/file.yaml",
		},
		{
			name: "clean pkg path",
			in:   pkgTempDir + "/../" + pkgTempDir + "/test.yaml",
			want: unzippedPkgDir + "/test.yaml",
		},
		{
			name: "clean instrumentation path",
			in:   instTempDir + "/../" + instTempDir + "/test.yaml",
			want: unzippedInstDir + "/test.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizePath(tt.in)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestExtract_TruncatesExistingFile(t *testing.T) {
	tmpDir := t.TempDir()

	targetPath := filepath.Join(tmpDir, "rules.yaml")

	err := os.WriteFile(targetPath, []byte("very long old content"), 0o644)
	require.NoError(t, err)

	newContent := []byte("new")

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)

	err = tw.WriteHeader(&tar.Header{
		Name:     "rules.yaml",
		Mode:     0o644,
		Size:     int64(len(newContent)),
		Typeflag: tar.TypeReg,
	})
	require.NoError(t, err)

	_, err = tw.Write(newContent)
	require.NoError(t, err)

	err = tw.Close()
	require.NoError(t, err)

	tr := tar.NewReader(bytes.NewReader(tarBuf.Bytes()))

	header, err := tr.Next()
	require.NoError(t, err)

	err = extract(tr, header, targetPath)
	require.NoError(t, err)

	bs, err := os.ReadFile(targetPath)
	require.NoError(t, err)

	require.Equal(t, string(newContent), string(bs))
}

func TestExtract_Directory(t *testing.T) {
	tmpDir := t.TempDir()

	target := filepath.Join(tmpDir, "dir")

	header := &tar.Header{
		Name:     "dir",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	}

	err := extract(nil, header, target)
	require.NoError(t, err)

	info, err := os.Stat(target)
	require.NoError(t, err)

	require.True(t, info.IsDir())
}

func TestExtract_UnsupportedType(t *testing.T) {
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)

	err := tw.WriteHeader(&tar.Header{
		Name:     "symlink",
		Typeflag: tar.TypeSymlink,
	})
	require.NoError(t, err)

	require.NoError(t, tw.Close())

	tr := tar.NewReader(bytes.NewReader(tarBuf.Bytes()))

	header, err := tr.Next()
	require.NoError(t, err)

	err = extract(tr, header, filepath.Join(t.TempDir(), "symlink"))
	require.Error(t, err)
}

func TestExtractGZip_Normal(t *testing.T) {
	tmpDir := t.TempDir()

	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)

	err := tw.WriteHeader(&tar.Header{
		Name:     "pkg_temp",
		Mode:     0o755,
		Typeflag: tar.TypeDir,
	})
	require.NoError(t, err)

	content := []byte("hello world")

	err = tw.WriteHeader(&tar.Header{
		Name:     "pkg_temp/test.yaml",
		Mode:     0o644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	})
	require.NoError(t, err)

	_, err = tw.Write(content)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	err = extractGZip(&tarBuf, tmpDir)
	require.NoError(t, err)

	bs, err := os.ReadFile(filepath.Join(tmpDir, "pkg", "test.yaml"))
	require.NoError(t, err)

	require.Equal(t, content, bs)
}

func TestExtractGZip_SkipsZipSlip(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		skipCheck func(t *testing.T, tmpDir string)
		checkPath func(tmpDir string) string
	}{
		{
			name: "relative traversal",
			path: "../evil.yaml",
			skipCheck: func(t *testing.T, tmpDir string) {
				if _, err := os.Stat(filepath.Join(tmpDir, "../evil.yaml")); err == nil {
					t.Skip("../evil.yaml already exists")
				}
			},
			checkPath: func(tmpDir string) string {
				return filepath.Join(tmpDir, "../evil.yaml")
			},
		},
		{
			name: "unix absolute path",
			path: "/evil.yaml",
			skipCheck: func(t *testing.T, tmpDir string) {
				if runtime.GOOS == "windows" {
					t.Skip("unix-specific test")
				}

				if _, err := os.Stat("/evil.yaml"); err == nil {
					t.Skip("/evil.yaml already exists")
				}
			},
			checkPath: func(tmpDir string) string {
				return "/evil.yaml"
			},
		},
		{
			name: "windows absolute path",
			path: "C:\\evil.yaml",
			skipCheck: func(t *testing.T, tmpDir string) {
				if runtime.GOOS != "windows" {
					t.Skip("windows-specific test")
				}

				if _, err := os.Stat("C:\\evil.yaml"); err == nil {
					t.Skip("C:\\evil.yaml already exists")
				}
			},
			checkPath: func(tmpDir string) string {
				return "C:\\evil.yaml"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			if tt.skipCheck != nil {
				tt.skipCheck(t, tmpDir)
			}

			var tarBuf bytes.Buffer
			gz := gzip.NewWriter(&tarBuf)
			tw := tar.NewWriter(gz)

			content := []byte("evil")

			err := tw.WriteHeader(&tar.Header{
				Name:     tt.path,
				Mode:     0o644,
				Size:     int64(len(content)),
				Typeflag: tar.TypeReg,
			})
			require.NoError(t, err)

			_, err = tw.Write(content)
			require.NoError(t, err)

			require.NoError(t, tw.Close())
			require.NoError(t, gz.Close())

			require.NoError(t, extractGZip(&tarBuf, tmpDir))

			checkPath := tt.checkPath(tmpDir)
			_, err = os.Stat(checkPath)

			require.True(t, os.IsNotExist(err))
		})
	}
}

func TestExtractGZip_InvalidGZip(t *testing.T) {
	tmpDir := t.TempDir()
	err := extractGZip(bytes.NewReader([]byte("not a gzip")), tmpDir)
	require.Error(t, err)
}

func TestExtractGZip_InvalidTar(t *testing.T) {
	tmpDir := t.TempDir()
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	_, err := gz.Write([]byte("not a tar file at all"))
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	err = extractGZip(&tarBuf, tmpDir)
	require.Error(t, err)
}

func TestExtractGZip_SkipsHiddenFiles(t *testing.T) {
	tmpDir := t.TempDir()
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)

	err := tw.WriteHeader(&tar.Header{
		Name:     ".hidden_file",
		Mode:     0o644,
		Size:     0,
		Typeflag: tar.TypeReg,
	})
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	err = extractGZip(&tarBuf, tmpDir)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tmpDir, ".hidden_file"))
	require.True(t, os.IsNotExist(err))
}

func TestExtractGZip_SkipsDotAndDotDot(t *testing.T) {
	tmpDir := t.TempDir()
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)

	err := tw.WriteHeader(&tar.Header{
		Name:     ".",
		Mode:     0o755,
		Typeflag: tar.TypeDir,
	})
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	err = extractGZip(&tarBuf, tmpDir)
	require.NoError(t, err)
}

func TestExtractBundle(t *testing.T) {
	err := extractBundle()
	require.NoError(t, err)
}

func TestExtract_OpenFileError(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "dir_conflict")
	err := os.Mkdir(targetPath, 0o755)
	require.NoError(t, err)

	header := &tar.Header{
		Name:     "dir_conflict",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     10,
	}

	err = extract(nil, header, targetPath)
	require.Error(t, err)
}

func TestExtract_MkdirAllError(t *testing.T) {
	tmpDir := t.TempDir()
	conflictPath := filepath.Join(tmpDir, "file_conflict")
	err := os.WriteFile(conflictPath, []byte("content"), 0o644)
	require.NoError(t, err)

	header := &tar.Header{
		Name:     "file_conflict/subdir",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	}

	err = extract(nil, header, filepath.Join(conflictPath, "subdir"))
	require.Error(t, err)
}

func TestExtract_CopyError(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "somefile")

	header := &tar.Header{
		Name:     "somefile",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     100,
	}

	tr := tar.NewReader(bytes.NewReader(nil))

	err := extract(tr, header, targetPath)
	require.Error(t, err)
}

func TestExtractGZip_MkdirAllError(t *testing.T) {
	tmpDir := t.TempDir()
	conflictPath := filepath.Join(tmpDir, "file_conflict")
	err := os.WriteFile(conflictPath, []byte("content"), 0o644)
	require.NoError(t, err)

	err = extractGZip(bytes.NewReader(nil), filepath.Join(conflictPath, "subdir"))
	require.Error(t, err)
}

func TestExtractGZip_ExtractError(t *testing.T) {
	tmpDir := t.TempDir()
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)

	// First write a regular file
	err := tw.WriteHeader(&tar.Header{
		Name:     "pkg_temp/conflict",
		Mode:     0o644,
		Size:     4,
		Typeflag: tar.TypeReg,
	})
	require.NoError(t, err)
	_, err = tw.Write([]byte("data"))
	require.NoError(t, err)

	// Now try to write a file inside that path as if it were a directory,
	// which will trigger MkdirAll error in extract()
	err = tw.WriteHeader(&tar.Header{
		Name:     "pkg_temp/conflict/nested",
		Mode:     0o644,
		Size:     4,
		Typeflag: tar.TypeReg,
	})
	require.NoError(t, err)
	_, err = tw.Write([]byte("data"))
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	err = extractGZip(&tarBuf, tmpDir)
	require.Error(t, err)
}
