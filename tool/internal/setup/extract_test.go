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

func TestExtract_ClosesFileOnCopyError(t *testing.T) {
	tmpDir := t.TempDir()

	targetPath := filepath.Join(tmpDir, "rules.yaml")

	// build a tar header that promises more bytes than the tar stream
	// actually contains, so io.CopyN fails partway through the copy.
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)

	err := tw.WriteHeader(&tar.Header{
		Name:     "rules.yaml",
		Mode:     0o644,
		Size:     100,
		Typeflag: tar.TypeReg,
	})
	require.NoError(t, err)

	_, err = tw.Write([]byte("short"))
	require.NoError(t, err)

	tr := tar.NewReader(bytes.NewReader(tarBuf.Bytes()))

	header, err := tr.Next()
	require.NoError(t, err)

	err = extract(tr, header, targetPath)
	require.Error(t, err)

	// extract writes a temporary file, and renames it only after a
	// successful copy. Thus the destination does not exist, and the
	// temporary file is removed. If extract() left the temporary file open,
	// the removal fails on Windows (the OS refuses to delete a file that
	// still has an open handle in this process).
	require.NoFileExists(t, targetPath)
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	require.Empty(t, entries, "the temporary file is removed")
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

// TestExtractGZipSkipsAbsoluteEntry: an archive entry with an absolute path
// is skipped. It is not written below the target directory either.
func TestExtractGZipSkipsAbsoluteEntry(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, extractGZip(bundleWithFile(t, "/abs.yaml", []byte("evil")), dir))
	require.NoFileExists(t, filepath.Join(dir, "abs.yaml"))
}

func TestExtractGZip_InvalidGzip(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	corrupted := bytes.NewReader([]byte("not gzip data"))

	err := extractGZip(corrupted, tmpDir)
	require.Error(t, err)
}

func TestExtractGZip_MkdirAllError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "file")
	err := os.WriteFile(filePath, []byte("content"), 0o644)
	require.NoError(t, err)

	// Passing an existing file path as targetDir causes os.MkdirAll to fail
	err = extractGZip(bytes.NewReader(nil), filePath)
	require.Error(t, err)
}

func TestExtractGZip_CloseError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, err := gw.Write([]byte("test data for gzip stream"))
	require.NoError(t, err)
	require.NoError(t, gw.Close())

	// Truncate the gzip trailer to trigger a gzip.Reader.Close error
	truncated := buf.Bytes()[:len(buf.Bytes())-4]

	tmpDir := t.TempDir()
	err = extractGZip(bytes.NewReader(truncated), tmpDir)
	require.Error(t, err)
}

// bundleWithFile returns a gzip tar archive with the regular file name.
func bundleWithFile(t *testing.T, name string, content []byte) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: name, Mode: 0o644, Typeflag: tar.TypeReg, Size: int64(len(content)),
	}))
	_, err := tw.Write(content)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return bytes.NewReader(buf.Bytes())
}

// TestExtractGZipDoesNotFollowLinkedParent: a symbolic link below the target
// directory (for example .otelc-build/pkg, restored from a forged state
// file) points outside it. The extraction must stop with an error, and must
// not write a new file at the target of the link. Before the fix, the
// destination file did not exist, so the check of the resolved path was not
// done.
func TestExtractGZipDoesNotFollowLinkedParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need a privilege on Windows")
	}
	scratch := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(scratch, unzippedPkgDir)))

	err := extractGZip(bundleWithFile(t, pkgTempDir+"/new.go", []byte("wrote outside")), scratch)
	require.Error(t, err)
	require.Contains(t, err.Error(), "symbolic link")
	require.NoFileExists(t, filepath.Join(outside, "new.go"))
}

// TestExtractGZipDoesNotFollowLinkedFile: a symbolic link at the destination
// file points outside the target directory. The extraction must stop with an
// error, and must not change the target of the link.
func TestExtractGZipDoesNotFollowLinkedFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need a privilege on Windows")
	}
	scratch := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim.go")
	require.NoError(t, os.WriteFile(victim, []byte("keep"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(scratch, unzippedPkgDir), 0o755))
	require.NoError(t, os.Symlink(victim, filepath.Join(scratch, unzippedPkgDir, "a.go")))

	require.Error(t, extractGZip(bundleWithFile(t, pkgTempDir+"/a.go", []byte("changed")), scratch))
	data, err := os.ReadFile(victim)
	require.NoError(t, err)
	require.Equal(t, "keep", string(data))
}

// TestExtractGZipDoesNotFollowLinkedRoot: the target directory itself (for
// example .otelc-build) is a symbolic link to a directory outside the
// project, which has a file at a path of the bundle. The extraction must stop
// with an error, and must not change or add files at the target of the link.
func TestExtractGZipDoesNotFollowLinkedRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need a privilege on Windows")
	}
	outside := t.TempDir()
	victim := filepath.Join(outside, unzippedPkgDir, "a.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(victim), 0o755))
	require.NoError(t, os.WriteFile(victim, []byte("keep"), 0o644))
	root := filepath.Join(t.TempDir(), ".otelc-build")
	require.NoError(t, os.Symlink(outside, root))

	err := extractGZip(bundleWithFile(t, pkgTempDir+"/a.go", []byte("changed")), root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "symbolic link")
	data, err := os.ReadFile(victim)
	require.NoError(t, err)
	require.Equal(t, "keep", string(data), "the file at the target of the link does not change")
}

// TestExtractGZipReplacesHardLink: the destination file is a hard link to a
// file outside the target directory. The extraction replaces the file, and
// the other file does not change.
func TestExtractGZipReplacesHardLink(t *testing.T) {
	scratch := t.TempDir()
	other := filepath.Join(t.TempDir(), "other.go")
	require.NoError(t, os.WriteFile(other, []byte("keep"), 0o644))
	dst := filepath.Join(scratch, unzippedPkgDir, "a.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	if err := os.Link(other, dst); err != nil {
		t.Skipf("no hard links: %v", err)
	}

	require.NoError(t, extractGZip(bundleWithFile(t, pkgTempDir+"/a.go", []byte("new")), scratch))
	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "new", string(data))
	data, err = os.ReadFile(other)
	require.NoError(t, err)
	require.Equal(t, "keep", string(data), "the hard link is replaced, not truncated")
}

// regularFileReader returns a tar reader at a regular file with content.
func regularFileReader(t *testing.T, content string) (*tar.Reader, *tar.Header) {
	t.Helper()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "rules.yaml",
		Mode:     0o644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}))
	_, err := tw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	tr := tar.NewReader(bytes.NewReader(tarBuf.Bytes()))
	header, err := tr.Next()
	require.NoError(t, err)
	return tr, header
}

// TestExtract_ParentIsFile: the directory of a file cannot be made below a
// regular file.
func TestExtract_ParentIsFile(t *testing.T) {
	tmpDir := t.TempDir()
	parent := filepath.Join(tmpDir, "file")
	require.NoError(t, os.WriteFile(parent, []byte("a file"), 0o644))

	tr, header := regularFileReader(t, "content")
	require.Error(t, extract(tr, header, filepath.Join(parent, "rules.yaml")))
	data, err := os.ReadFile(parent)
	require.NoError(t, err)
	require.Equal(t, "a file", string(data))
}

// TestExtract_TargetIsDirectory: a file does not replace a directory that
// has files. The temporary file is removed.
func TestExtract_TargetIsDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "rules.yaml")
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "keep"), []byte("keep"), 0o644))

	tr, header := regularFileReader(t, "content")
	require.Error(t, extract(tr, header, target))
	require.FileExists(t, filepath.Join(target, "keep"))
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the temporary file is removed")
}
