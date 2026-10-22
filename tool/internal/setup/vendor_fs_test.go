// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"

	"go.opentelemetry.io/otelc/tool/util"
)

func skipWithoutSymlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need special permissions on Windows")
	}
}

func TestCheckNoSymlink(t *testing.T) {
	skipWithoutSymlinks(t)
	root := t.TempDir()
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "vendor", "a", "file.go"), "package a")
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "vendor", "linked")))
	require.NoError(t, os.Symlink(filepath.Join(outside, "absent"), filepath.Join(root, "vendor", "a", "dangling.go")))

	require.NoError(t, checkNoSymlink(root, filepath.Join(root, "vendor", "a", "file.go")))
	require.NoError(t, checkNoSymlink(root, filepath.Join(root, "vendor", "a", "new", "x.go")),
		"a path that does not exist is not a link")
	require.Error(t, checkNoSymlink(root, filepath.Join(root, "vendor", "a", "dangling.go")),
		"a dangling link is a link")
	require.Error(t, checkNoSymlink(root, filepath.Join(root, "vendor", "linked", "x.go")),
		"a linked parent directory is a link")
	require.Error(t, checkNoSymlink(root, filepath.Join(root, "..", "x")), "a path outside root is an error")
}

func TestCopyNewFileDoesNotFollowLinks(t *testing.T) {
	skipWithoutSymlinks(t)
	root := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(t.TempDir(), "src.go")
	mustWriteFile(t, src, "package x")

	dangling := filepath.Join(root, "vendor", "dangling.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(dangling), 0o755))
	target := filepath.Join(outside, "created-outside.go")
	require.NoError(t, os.Symlink(target, dangling))
	require.Error(t, copyNewFile(filepath.Dir(src), src, root, dangling))
	assert.NoFileExists(t, target, "no file is written through a dangling link")

	require.NoError(t, os.Symlink(outside, filepath.Join(root, "vendor", "linked")))
	require.Error(t, copyNewFile(filepath.Dir(src), src, root, filepath.Join(root, "vendor", "linked", "x.go")))
	assert.NoFileExists(t, filepath.Join(outside, "x.go"), "no file is written in a linked directory")

	existing := filepath.Join(root, "vendor", "existing.go")
	mustWriteFile(t, existing, "package user")
	require.Error(t, copyNewFile(filepath.Dir(src), src, root, existing), "an existing file is never replaced")
	data, err := os.ReadFile(existing)
	require.NoError(t, err)
	assert.Equal(t, "package user", string(data))

	dst := filepath.Join(root, "vendor", "new", "x.go")
	require.NoError(t, copyNewFile(filepath.Dir(src), src, root, dst))
	data, err = os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "package x", string(data))
}

// TestPlanVendorCopiesRejectsLinks: an attacker puts a dangling link (or a
// linked directory) at a path where otelc adds a package. The merge must fail
// before it writes anything.
func TestPlanVendorCopiesRejectsLinks(t *testing.T) {
	skipWithoutSymlinks(t)
	merged := mustParseModulesTxt(t, "# example.com/m v1.0.0\n## explicit\nexample.com/m/sub\n")

	t.Run("dangling link", func(t *testing.T) {
		scratch := t.TempDir()
		moduleDir := t.TempDir()
		mustWriteFile(t, filepath.Join(scratch, "example.com", "m", "sub", "a.go"), "package sub")
		link := filepath.Join(moduleDir, vendorDirName, "example.com", "m", "sub", "a.go")
		require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o755))
		require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "outside.go"), link))

		_, _, err := planVendorCopies(merged, []string{"example.com/m/sub"},
			vendorCopyRoots{scratchVendor: scratch, moduleDir: moduleDir})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "symbolic link")
	})

	t.Run("linked parent directory", func(t *testing.T) {
		scratch := t.TempDir()
		moduleDir := t.TempDir()
		mustWriteFile(t, filepath.Join(scratch, "example.com", "m", "sub", "a.go"), "package sub")
		require.NoError(t, os.MkdirAll(filepath.Join(moduleDir, vendorDirName), 0o755))
		require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(moduleDir, vendorDirName, "example.com")))

		_, _, err := planVendorCopies(merged, []string{"example.com/m/sub"},
			vendorCopyRoots{scratchVendor: scratch, moduleDir: moduleDir})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "symbolic link")
	})
}

func TestCopyVendoredModuleRejectsLinks(t *testing.T) {
	skipWithoutSymlinks(t)
	moduleDir := t.TempDir()
	scratchDir := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret.txt")
	mustWriteFile(t, secret, "secret")
	mustWriteFile(t, filepath.Join(moduleDir, vendorDirName, "example.com", "m", "a.go"), "package m")
	require.NoError(t, os.Symlink(secret, filepath.Join(moduleDir, vendorDirName, "example.com", "m", "link.go")))

	mod := &vendorModule{Path: "example.com/m", Version: "v1.0.0", Packages: []string{"example.com/m"}}
	err := copyVendoredModule(mod, moduleDir, scratchDir, filepath.Join(scratchDir, "deps", "0000"), vendoredDirs{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symbolic link")
	assert.NoFileExists(t, filepath.Join(scratchDir, "deps", "0000", "link.go"))
}

// TestParseModulesTxtRejectsBadPaths: otelc makes file paths from the lines
// of vendor/modules.txt. A line such as "../secrets" must not get in.
func TestParseModulesTxtRejectsBadPaths(t *testing.T) {
	tests := map[string]string{
		"parent package":       "# example.com/m v1.0.0\n## explicit\n../secrets\n",
		"parent below module":  "# example.com/m v1.0.0\n## explicit\nexample.com/m/../../../outside\n",
		"absolute package":     "# example.com/m v1.0.0\n## explicit\n/etc\n",
		"package of other mod": "# example.com/m v1.0.0\n## explicit\nexample.com/other\n",
		"bad module path":      "# ../m v1.0.0\n## explicit\nexample.com/m\n",
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseModulesTxt([]byte(data))
			require.Error(t, err)
		})
	}
}

// TestApplyVendorMergeKeepsModulesTxtMode: the user can make modules.txt and
// go.mod private (0600). otelc vendor must not make them readable by all.
func TestApplyVendorMergeKeepsModulesTxtMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are different on Windows")
	}
	moduleDir := t.TempDir()
	scratchVendor := t.TempDir()
	modulesTxtPath := filepath.Join(moduleDir, vendorDirName, modulesTxtFileName)
	mustWriteFile(t, modulesTxtPath, "# example.com/a v1.0.0\n## explicit\nexample.com/a\n")
	require.NoError(t, os.Chmod(modulesTxtPath, 0o600))
	goModPath := filepath.Join(moduleDir, goModFileName)
	mustWriteFile(t, goModPath, "module example.com/app\n\ngo 1.25.0\n")
	require.NoError(t, os.Chmod(goModPath, 0o600))
	mustWriteFile(t, filepath.Join(scratchVendor, "example.com", "b", "b.go"), "package b")

	mf, err := modfile.Parse(goModFileName, []byte("module example.com/app\n\ngo 1.25.0\n"), nil)
	require.NoError(t, err)
	merged := mustParseModulesTxt(t,
		"# example.com/a v1.0.0\n## explicit\nexample.com/a\n# example.com/b v1.0.0\n## explicit\nexample.com/b\n")
	require.NoError(t, applyVendorMerge(t.Context(), moduleDir, scratchVendor, vendorMergeResult{
		goMod:  mf,
		merged: merged,
		added:  []string{"example.com/b"},
	}))

	info, err := os.Stat(modulesTxtPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	info, err = os.Stat(goModPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the mode of go.mod is kept too")
	assert.FileExists(t, filepath.Join(moduleDir, vendorDirName, "example.com", "b", "b.go"))
}

// TestApplyVendorMergeReplacesDeletedFiles: an updated module gets the files
// of its new version. Its old files are removed first.
func TestApplyVendorMergeReplacesDeletedFiles(t *testing.T) {
	moduleDir := t.TempDir()
	scratchVendor := t.TempDir()
	mustWriteFile(t, filepath.Join(moduleDir, vendorDirName, modulesTxtFileName),
		"# example.com/a v1.0.0\n## explicit\nexample.com/a\n")
	mustWriteFile(t, filepath.Join(moduleDir, goModFileName), "module example.com/app\n\ngo 1.25.0\n")
	oldFile := filepath.Join(moduleDir, vendorDirName, "example.com", "a", "old.go")
	keptName := filepath.Join(moduleDir, vendorDirName, "example.com", "a", "a.go")
	mustWriteFile(t, oldFile, "package a // old only")
	mustWriteFile(t, keptName, "package a // v1.0.0")
	mustWriteFile(t, filepath.Join(scratchVendor, "example.com", "a", "a.go"), "package a // v1.2.0")

	user := mustParseModulesTxt(t, "# example.com/a v1.0.0\n## explicit\nexample.com/a\n")
	deleted, err := vendoredModuleFiles(user, map[string]bool{"example.com/a": true}, moduleDir)
	require.NoError(t, err)
	assert.Equal(t, []string{keptName, oldFile}, deleted)

	mf, err := modfile.Parse(goModFileName, []byte("module example.com/app\n\ngo 1.25.0\n"), nil)
	require.NoError(t, err)
	merged := mustParseModulesTxt(t, "# example.com/a v1.2.0\n## explicit\nexample.com/a\n")
	require.NoError(t, applyVendorMerge(t.Context(), moduleDir, scratchVendor, vendorMergeResult{
		goMod:   mf,
		merged:  merged,
		added:   []string{"example.com/a"},
		deleted: deleted,
	}))

	assert.NoFileExists(t, oldFile)
	data, err := os.ReadFile(keptName)
	require.NoError(t, err)
	assert.Equal(t, "package a // v1.2.0", string(data))
}

// TestApplyVendorMergeFailureIsReverted: a copy fails after otelc removed the
// old files of an updated module and copied some new files. The tracked
// state restores the module of the user exactly.
func TestApplyVendorMergeFailureIsReverted(t *testing.T) {
	skipWithoutPermissions(t)
	moduleDir := t.TempDir()
	t.Chdir(moduleDir)
	t.Setenv(util.EnvOtelcWorkDir, moduleDir)
	t.Setenv("GOWORK", "off")
	scratchVendor := t.TempDir()
	modulesTxt := "# example.com/a v1.0.0\n## explicit\nexample.com/a\n"
	goMod := "module example.com/app\n\ngo 1.25.0\n"
	mustWriteFile(t, filepath.Join(moduleDir, vendorDirName, modulesTxtFileName), modulesTxt)
	mustWriteFile(t, filepath.Join(moduleDir, goModFileName), goMod)
	oldFile := filepath.Join(moduleDir, vendorDirName, "example.com", "a", "a.go")
	mustWriteFile(t, oldFile, "package a // patched v1.0.0")
	mustWriteFile(t, filepath.Join(scratchVendor, "example.com", "a", "a.go"), "package a // v1.2.0")
	unreadable := filepath.Join(scratchVendor, "example.com", "a", "z.go")
	mustWriteFile(t, unreadable, "package a")
	require.NoError(t, os.Chmod(unreadable, 0o000))

	user := mustParseModulesTxt(t, modulesTxt)
	deleted, err := vendoredModuleFiles(user, map[string]bool{"example.com/a": true}, moduleDir)
	require.NoError(t, err)
	mf, err := modfile.Parse(goModFileName, []byte(goMod), nil)
	require.NoError(t, err)
	require.NoError(t, mf.AddRequire("example.com/a", "v1.2.0"))

	stateManager := newStateManager()
	ctx := contextWithStateManager(t.Context(), stateManager)
	err = applyVendorMerge(ctx, moduleDir, scratchVendor, vendorMergeResult{
		goMod:   mf,
		merged:  mustParseModulesTxt(t, "# example.com/a v1.2.0\n## explicit\nexample.com/a\n"),
		added:   []string{"example.com/a"},
		deleted: deleted,
	})
	require.Error(t, err, "the copy of the unreadable file fails")
	data, err := os.ReadFile(oldFile)
	require.NoError(t, err)
	require.Equal(t, "package a // v1.2.0", string(data), "the old file was replaced before the failure")

	require.NoError(t, revertState(stateManager))
	data, err = os.ReadFile(oldFile)
	require.NoError(t, err)
	assert.Equal(t, "package a // patched v1.0.0", string(data), "the patched file of the user is back")
	assert.NoFileExists(t, filepath.Join(moduleDir, vendorDirName, "example.com", "a", "z.go"))
	data, err = os.ReadFile(filepath.Join(moduleDir, vendorDirName, modulesTxtFileName))
	require.NoError(t, err)
	assert.Equal(t, modulesTxt, string(data))
	data, err = os.ReadFile(filepath.Join(moduleDir, goModFileName))
	require.NoError(t, err)
	assert.Equal(t, goMod, string(data))
}

func TestVendorFSErrors(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "vendor", "dir")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(root, "vendor", "file.go")
	mustWriteFile(t, file, "package x")

	require.Error(t, copyNewFile(root, dir, root, filepath.Join(root, "vendor", "copy")),
		"a directory is not a regular file")
	require.Error(t, copyNewFile(root, filepath.Join(root, "absent.go"), root, filepath.Join(root, "vendor", "c.go")),
		"a missing source")
	require.Error(t, copyNewFile(root, file, root, filepath.Join(file, "below.go")),
		"the parent of the destination is a file")
	require.Error(t, copyNewFile(root, file, root, filepath.Join(t.TempDir(), "outside.go")),
		"a destination outside root")

	require.NoError(t, removeVendoredFile(root, filepath.Join(root, "vendor", "absent.go")),
		"a file that does not exist is not an error")
	require.Error(t, removeVendoredFile(root, dir), "a directory is not removed")
	require.Error(t, removeVendoredFile(root, filepath.Join(t.TempDir(), "x")), "a path outside root")
	require.NoError(t, removeVendoredFile(root, file))
	assert.NoFileExists(t, file)

	files, err := regularFiles(root, filepath.Join(root, "vendor", "absent"))
	require.NoError(t, err)
	assert.Empty(t, files, "a directory that does not exist has no files")
	if runtime.GOOS != "windows" {
		// On Windows, reading a file as a directory gives a "not found" error.
		mustWriteFile(t, file, "package x")
		_, err = regularFiles(root, file)
		require.Error(t, err, "a file is not a directory")
	}
}

func TestRemoveVendoredFileRejectsLink(t *testing.T) {
	skipWithoutSymlinks(t)
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.go")
	mustWriteFile(t, target, "package x")
	link := filepath.Join(root, "vendor", "link.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o755))
	require.NoError(t, os.Symlink(target, link))

	require.Error(t, removeVendoredFile(root, link))
	assert.FileExists(t, target)
	_, err := regularFiles(root, filepath.Dir(link))
	require.Error(t, err, "a link in a vendored directory is an error")
}

func TestVendorFSPermissionErrors(t *testing.T) {
	skipWithoutPermissions(t)
	root := t.TempDir()
	src := filepath.Join(root, "src.go")
	mustWriteFile(t, src, "package x")
	locked := filepath.Join(root, "vendor", "locked")
	require.NoError(t, os.MkdirAll(locked, 0o755))
	file := filepath.Join(locked, "file.go")
	mustWriteFile(t, file, "package x")
	require.NoError(t, os.Chmod(locked, 0o555))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	require.Error(t, copyNewFile(root, filepath.Join(t.TempDir(), "x.go"), root, filepath.Join(root, "vendor", "c.go")),
		"a source outside its root")
	require.Error(t, copyNewFile(root, src, root, filepath.Join(locked, "sub", "x.go")),
		"the directory cannot be made")
	require.Error(t, copyNewFile(root, src, root, filepath.Join(locked, "x.go")),
		"the file cannot be made")
	require.Error(t, removeVendoredFile(root, file), "the file cannot be removed")
	err := linkNewFile(root, src, root, filepath.Join(locked, "sub", "x.go"))
	require.Error(t, err, "the directory of the link cannot be made")
	assert.Contains(t, err.Error(), "creating the directory of "+filepath.Join(locked, "sub", "x.go"))
}

// relFiles returns the paths of files relative to root, slash-separated.
func relFiles(t *testing.T, root string, files []string) []string {
	t.Helper()
	out := make([]string, 0, len(files))
	for _, f := range files {
		rel, err := filepath.Rel(root, f)
		require.NoError(t, err)
		out = append(out, filepath.ToSlash(rel))
	}
	slices.Sort(out)
	return out
}

// embedVendorTree writes a vendor directory below root with the module
// example.com/m: the package example.com/m embeds files in assets/ and in
// assets/deep/, example.com/m/sub is a different package, and
// example.com/m/nested is the root of a different module.
func embedVendorTree(t *testing.T, root string) (*modulesTxt, string) {
	t.Helper()
	vendor := filepath.Join(root, vendorDirName)
	for name, content := range map[string]string{
		"example.com/m/m.go":                  "package m",
		"example.com/m/assets/a.txt":          "a",
		"example.com/m/assets/deep/b.txt":     "b",
		"example.com/m/sub/sub.go":            "package sub",
		"example.com/m/nested/LICENSE":        "nested license",
		"example.com/m/nested/pkg/pkg.go":     "package pkg",
		"example.com/m/sub/assets/c.txt":      "c",
		"example.com/other/other.go":          "package other",
		"example.com/m/assets/deep/more/d.md": "d",
	} {
		mustWriteFile(t, filepath.Join(vendor, filepath.FromSlash(name)), content)
	}
	m := mustParseModulesTxt(t, `# example.com/m v1.0.0
## explicit
example.com/m
example.com/m/sub
# example.com/m/nested v1.0.0
## explicit
example.com/m/nested/pkg
# example.com/other v1.0.0
## explicit
example.com/other
`)
	return m, vendor
}

func TestPackageFiles(t *testing.T) {
	root := t.TempDir()
	m, vendor := embedVendorTree(t, root)
	own := vendoredDirsOf(m)

	files, err := packageFiles(root, filepath.Join(vendor, "example.com", "m"), "example.com/m", own)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"example.com/m/assets/a.txt",
		"example.com/m/assets/deep/b.txt",
		"example.com/m/assets/deep/more/d.md",
		"example.com/m/m.go",
	}, relFiles(t, vendor, files), "the embedded files are included; a package and a module root are not")

	files, err = packageFiles(root, filepath.Join(vendor, "example.com", "m", "sub"), "example.com/m/sub", own)
	require.NoError(t, err)
	assert.Equal(t, []string{"example.com/m/sub/assets/c.txt", "example.com/m/sub/sub.go"},
		relFiles(t, vendor, files))

	files, err = packageFiles(root, filepath.Join(vendor, "example.com", "absent"), "example.com/absent", own)
	require.NoError(t, err)
	assert.Empty(t, files, "a directory that does not exist has no files")

	if runtime.GOOS != "windows" {
		require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(vendor, "example.com", "m", "assets", "deep", "link")))
		_, err = packageFiles(root, filepath.Join(vendor, "example.com", "m"), "example.com/m", own)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "symbolic link", "a link in an embedded subdirectory is an error")
	}
	_, err = packageFiles(root, filepath.Join(t.TempDir(), "x"), "example.com/x", own)
	require.Error(t, err, "a directory outside root")
}

func TestVendoredModuleFilesEmbeddedSubdirectories(t *testing.T) {
	root := t.TempDir()
	m, vendor := embedVendorTree(t, root)
	mustWriteFile(t, filepath.Join(vendor, "example.com", "m", "nested", "pkg", "deep", "x.txt"), "x")

	files, err := vendoredModuleFiles(m, map[string]bool{"example.com/m": true}, root)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"example.com/m/assets/a.txt",
		"example.com/m/assets/deep/b.txt",
		"example.com/m/assets/deep/more/d.md",
		"example.com/m/m.go",
		"example.com/m/sub/assets/c.txt",
		"example.com/m/sub/sub.go",
	}, relFiles(t, vendor, files), "the files of the module example.com/m/nested stay")

	files, err = vendoredModuleFiles(m, map[string]bool{"example.com/m/nested": true}, root)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"example.com/m/nested/LICENSE",
		"example.com/m/nested/pkg/deep/x.txt",
		"example.com/m/nested/pkg/pkg.go",
	}, relFiles(t, vendor, files), "the module root is not a package: its LICENSE is a file of the module")
}

func TestPlanVendorCopiesEmbeddedSubdirectories(t *testing.T) {
	scratchRoot := t.TempDir()
	m, scratchVendor := embedVendorTree(t, scratchRoot)
	moduleDir := t.TempDir()
	userVendor := filepath.Join(moduleDir, vendorDirName)
	mustWriteFile(t, filepath.Join(userVendor, "example.com", "m", "assets", "a.txt"), "user a")

	copies, dirs, err := planVendorCopies(m, []string{"example.com/m"},
		vendorCopyRoots{scratchVendor: scratchVendor, moduleDir: moduleDir})
	require.NoError(t, err)
	dsts := make([]string, 0, len(copies))
	for _, c := range copies {
		dsts = append(dsts, c.dst)
	}
	assert.Equal(t, []string{
		"example.com/m/assets/deep/b.txt",
		"example.com/m/assets/deep/more/d.md",
		"example.com/m/m.go",
	}, relFiles(t, userVendor, dsts), "an existing file is never replaced")
	assert.Equal(t, []string{
		filepath.Join(userVendor, "example.com", "m", "assets", "deep"),
		filepath.Join(userVendor, "example.com", "m", "assets", "deep", "more"),
	}, dirs, "the missing subdirectories are made, parents first")
}

func TestCopyVendoredModuleLinksEmbeddedFiles(t *testing.T) {
	moduleDir := t.TempDir()
	m, vendor := embedVendorTree(t, moduleDir)
	// "go mod vendor" of go < 1.17 copies go.mod.
	vendoredGoMod := filepath.Join(vendor, "example.com", "m", goModFileName)
	mustWriteFile(t, vendoredGoMod, "module example.com/m\n\nrequire example.com/x v1.0.0\n")
	scratchDir := t.TempDir()
	dst := filepath.Join(scratchDir, "deps", "0000")
	useClones, useHardLinks = false, true
	t.Cleanup(func() { useClones, useHardLinks = runtime.GOOS == "darwin", runtime.GOOS == "linux" })

	require.NoError(t, copyVendoredModule(m.find("example.com/m"), moduleDir, scratchDir, dst, vendoredDirsOf(m)))

	var files []string
	require.NoError(t, filepath.WalkDir(dst, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return err
	}))
	assert.Equal(t, []string{
		"assets/a.txt", "assets/deep/b.txt", "assets/deep/more/d.md", "go.mod", "m.go",
		"sub/assets/c.txt", "sub/sub.go",
	}, relFiles(t, dst, files), "the copy has the embedded files, and not the files of a different module")

	src, err := os.Stat(filepath.Join(vendor, "example.com", "m", "assets", "deep", "b.txt"))
	require.NoError(t, err)
	copied, err := os.Stat(filepath.Join(dst, "assets", "deep", "b.txt"))
	require.NoError(t, err)
	assert.True(t, os.SameFile(src, copied), "the copy is a hard link")

	assert.Equal(t, "module example.com/m\n", readAppFile(t, dst, goModFileName), "the copy has a new go.mod file")
	assert.Equal(t, "module example.com/m\n\nrequire example.com/x v1.0.0\n",
		readAppFile(t, vendor, "example.com/m/go.mod"), "the vendored go.mod file does not change")
	srcGoMod, err := os.Stat(vendoredGoMod)
	require.NoError(t, err)
	copiedGoMod, err := os.Stat(filepath.Join(dst, goModFileName))
	require.NoError(t, err)
	assert.False(t, os.SameFile(srcGoMod, copiedGoMod), "the go.mod file of the copy is not a link")
}

func TestLinkNewFile(t *testing.T) {
	useClones, useHardLinks = false, true
	t.Cleanup(func() { useClones, useHardLinks = runtime.GOOS == "darwin", runtime.GOOS == "linux" })
	root := t.TempDir()
	src := filepath.Join(root, "vendor", "a.go")
	mustWriteFile(t, src, "package a")
	dstRoot := t.TempDir()
	dst := filepath.Join(dstRoot, "deps", "a.go")

	require.NoError(t, linkNewFile(root, src, dstRoot, dst))
	assert.Equal(t, "package a", readAppFile(t, dstRoot, "deps/a.go"))
	srcInfo, err := os.Stat(src)
	require.NoError(t, err)
	dstInfo, err := os.Stat(dst)
	require.NoError(t, err)
	assert.True(t, os.SameFile(srcInfo, dstInfo), "dst is a hard link")
	require.Error(t, linkNewFile(root, src, dstRoot, dst), "an existing file is never replaced")
	require.Error(t, linkNewFile(root, filepath.Join(root, "vendor"), dstRoot, filepath.Join(dstRoot, "d")),
		"a directory is not linked")
	require.Error(t, linkNewFile(root, filepath.Join(root, "absent.go"), dstRoot, filepath.Join(dstRoot, "x")),
		"a source that does not exist")
	require.Error(t, linkNewFile(root, src, dstRoot, filepath.Join(t.TempDir(), "x.go")),
		"a destination outside its root")
	require.Error(t, linkNewFile(root, filepath.Join(t.TempDir(), "x.go"), dstRoot, filepath.Join(dstRoot, "x")),
		"a source outside its root")
	mustWriteFile(t, filepath.Join(dstRoot, "file"), "x")
	require.Error(t, linkNewFile(root, src, dstRoot, filepath.Join(dstRoot, "file", "below.go")),
		"the parent of the destination is a file")

	if runtime.GOOS != "windows" {
		link := filepath.Join(root, "vendor", "link.go")
		require.NoError(t, os.Symlink(src, link))
		require.Error(t, linkNewFile(root, link, dstRoot, filepath.Join(dstRoot, "link.go")),
			"a symbolic link is not followed")
	}
}

// TestLinkNewFileCopiesWhenLinkFails: when a hard link is not possible (for
// example, a different file system), linkNewFile copies the file. Without
// useHardLinks, it copies the file too.
func TestLinkNewFileCopiesWhenLinkFails(t *testing.T) {
	useClones, useHardLinks = false, true
	osLink = func(string, string) error { return &os.LinkError{Op: "link", Err: os.ErrPermission} }
	t.Cleanup(func() {
		osLink = os.Link
		useClones, useHardLinks = runtime.GOOS == "darwin", runtime.GOOS == "linux"
	})
	root := t.TempDir()
	src := filepath.Join(root, "a.go")
	mustWriteFile(t, src, "package a")
	dstRoot := t.TempDir()
	dst := filepath.Join(dstRoot, "deps", "a.go")

	require.NoError(t, linkNewFile(root, src, dstRoot, dst))
	srcInfo, err := os.Stat(src)
	require.NoError(t, err)
	dstInfo, err := os.Stat(dst)
	require.NoError(t, err)
	assert.False(t, os.SameFile(srcInfo, dstInfo), "the file is a copy")
	assert.Equal(t, "package a", readAppFile(t, dstRoot, "deps/a.go"))
	require.Error(t, linkNewFile(root, src, dstRoot, dst), "the copy never replaces a file")

	useHardLinks = false
	osLink = func(string, string) error {
		t.Error("no link without useHardLinks")
		return os.ErrPermission
	}
	other := filepath.Join(dstRoot, "deps", "b.go")
	require.NoError(t, linkNewFile(root, src, dstRoot, other))
	otherInfo, err := os.Stat(other)
	require.NoError(t, err)
	assert.False(t, os.SameFile(srcInfo, otherInfo), "the file is a copy")
}

// TestLinkNewFileClones: with useClones on macOS, dst is a clone of src: a
// different file with the same content. A write to dst does not change src.
func TestLinkNewFileClones(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("clonefile(2) is only on macOS")
	}
	useClones, useHardLinks = true, false
	t.Cleanup(func() { useClones, useHardLinks = runtime.GOOS == "darwin", runtime.GOOS == "linux" })
	root := t.TempDir()
	src := filepath.Join(root, "vendor", "a.go")
	mustWriteFile(t, src, "package a")
	dstRoot := t.TempDir()
	dst := filepath.Join(dstRoot, "deps", "a.go")
	// The test needs a file system with clones (APFS).
	if err := platformCloneFile(src, filepath.Join(root, "probe")); err != nil {
		t.Skipf("the temporary directory does not support clones: %v", err)
	}

	require.NoError(t, linkNewFile(root, src, dstRoot, dst))
	srcInfo, err := os.Stat(src)
	require.NoError(t, err)
	dstInfo, err := os.Stat(dst)
	require.NoError(t, err)
	assert.False(t, os.SameFile(srcInfo, dstInfo), "a clone is not a hard link")
	assert.Equal(t, srcInfo.Mode().Perm(), dstInfo.Mode().Perm(), "the clone keeps the mode")
	require.NoError(t, os.WriteFile(dst, []byte("package changed"), 0o644))
	assert.Equal(t, "package a", readAppFile(t, root, "vendor/a.go"), "a write to the clone does not change src")
	require.Error(t, linkNewFile(root, src, dstRoot, dst), "an existing file is never replaced")

	if runtime.GOOS != "windows" {
		link := filepath.Join(root, "vendor", "link.go")
		require.NoError(t, os.Symlink(src, link))
		require.Error(t, linkNewFile(root, link, dstRoot, filepath.Join(dstRoot, "link.go")),
			"a symbolic link is not followed")
	}
}

// TestLinkNewFileFallsBackWhenCloneFails: when a clone is not possible (for
// example, a file system without clones), linkNewFile tries a hard link, and
// then a copy. A clone or a link is not tried when it is not enabled.
func TestLinkNewFileFallsBackWhenCloneFails(t *testing.T) {
	var calls []string
	cloneFile = func(string, string) error {
		calls = append(calls, "clone")
		return errors.ErrUnsupported
	}
	osLink = func(string, string) error {
		calls = append(calls, "link")
		return &os.LinkError{Op: "link", Err: os.ErrPermission}
	}
	t.Cleanup(func() {
		cloneFile, osLink = platformCloneFile, os.Link
		useClones, useHardLinks = runtime.GOOS == "darwin", runtime.GOOS == "linux"
	})
	root := t.TempDir()
	src := filepath.Join(root, "a.go")
	mustWriteFile(t, src, "package a")
	dstRoot := t.TempDir()

	for i, tc := range []struct {
		clones, links bool
		want          []string
	}{
		{clones: true, links: true, want: []string{"clone", "link"}},
		{clones: true, want: []string{"clone"}},
		{links: true, want: []string{"link"}},
		{},
	} {
		useClones, useHardLinks = tc.clones, tc.links
		calls = nil
		dst := filepath.Join(dstRoot, "deps", fmt.Sprintf("%d.go", i))
		require.NoError(t, linkNewFile(root, src, dstRoot, dst))
		assert.Equal(t, tc.want, calls, "clones=%v links=%v", tc.clones, tc.links)
		assert.Equal(t, "package a", readAppFile(t, dstRoot, fmt.Sprintf("deps/%d.go", i)), "the file is copied")
	}
}

// TestPlatformCloneFile: on a system without clones, platformCloneFile
// fails and makes no file.
func TestPlatformCloneFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.go")
	mustWriteFile(t, src, "package a")
	dst := filepath.Join(root, "b.go")
	err := platformCloneFile(src, dst)
	if runtime.GOOS != "darwin" {
		require.ErrorIs(t, err, errors.ErrUnsupported)
		assert.NoFileExists(t, dst)
		return
	}
	if err != nil {
		t.Skipf("the temporary directory does not support clones: %v", err)
	}
	assert.Equal(t, "package a", readAppFile(t, root, "b.go"))
	require.Error(t, platformCloneFile(src, dst), "a clone never replaces a file")
}

// BenchmarkCopyVendoredModules measures the copy of a large vendor/ (about
// the size of the vendor/ of Kubernetes: 150 modules, 1,500 packages, 19,500
// files of 8 KiB) into the scratch module, with clones, with hard links and
// with copies (see useClones and useHardLinks). On a system without clones
// (not macOS, or not APFS), the "clone" case falls back to copies.
//
//	go test ./internal/setup/ -run '^$' -bench BenchmarkCopyVendoredModules -benchtime 5x
func BenchmarkCopyVendoredModules(b *testing.B) {
	const (
		modules        = 150
		pkgsPerModule  = 10
		filesPerPkg    = 13
		fileSize       = 8 * 1024
		benchGoVersion = "1.21"
	)
	moduleDir := b.TempDir()
	content := strings.Repeat("x", fileSize)
	var modulesTxt strings.Builder
	for m := range modules {
		modPath := fmt.Sprintf("example.com/mod%03d", m)
		fmt.Fprintf(&modulesTxt, "# %s v1.0.0\n## explicit; go %s\n", modPath, benchGoVersion)
		for p := range pkgsPerModule {
			pkg := fmt.Sprintf("%s/pkg%02d", modPath, p)
			modulesTxt.WriteString(pkg + "\n")
			dir := filepath.Join(moduleDir, vendorDirName, filepath.FromSlash(pkg))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				b.Fatal(err)
			}
			for f := range filesPerPkg {
				if err := os.WriteFile(
					filepath.Join(dir, fmt.Sprintf("f%02d.go", f)),
					[]byte(content),
					0o644,
				); err != nil {
					b.Fatal(err)
				}
			}
		}
	}
	user, err := parseModulesTxt([]byte(modulesTxt.String()))
	if err != nil {
		b.Fatal(err)
	}
	in := vendorScratchInput{user: user, original: user, userVendorDir: filepath.Join(moduleDir, vendorDirName)}
	// The scratch module is in the module directory, like .otelc-build.
	scratch := filepath.Join(moduleDir, util.BuildTempDir, vendorScratchDir)

	modes := []struct {
		name          string
		clones, links bool
	}{
		{name: "clone", clones: true},
		{name: "link", links: true},
		{name: "copy"},
	}
	for _, mode := range modes {
		b.Run(mode.name, func(b *testing.B) {
			useClones, useHardLinks = mode.clones, mode.links
			b.Cleanup(func() { useClones, useHardLinks = runtime.GOOS == "darwin", runtime.GOOS == "linux" })
			for b.Loop() {
				b.StopTimer()
				if err = os.RemoveAll(scratch); err != nil {
					b.Fatal(err)
				}
				if err = os.MkdirAll(scratch, 0o755); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if _, _, err = copyVendoredModules(scratch, in); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
