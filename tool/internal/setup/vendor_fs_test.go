// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"runtime"
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
	err := copyVendoredModule(mod, moduleDir, scratchDir, filepath.Join(scratchDir, "deps", "0000"))
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
	if runtime.GOOS == "windows" {
		t.Skip("file modes are different on Windows")
	}
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
