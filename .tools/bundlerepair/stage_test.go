// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type stageFixture struct {
	branch  string
	trusted string
	baseSHA string
}

// testSHA is a structurally valid commit SHA for argument tests.
const testSHA = "0123456789abcdef0123456789abcdef01234567"

// wantBaseMoved is the fail-closed message that staging must print.
const wantBaseMoved = "main moved after the request was prepared; request the repair again"

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newStageFixture mirrors stage_fixture in the old shell test. The trusted
// directory is a git checkout at baseSHA.
func newStageFixture(t *testing.T) stageFixture {
	t.Helper()
	fx := stageFixture{branch: t.TempDir(), trusted: t.TempDir()}
	for _, root := range []string{fx.branch, fx.trusted} {
		write(t, filepath.Join(root, "pkg/sub/kept.go"), "package pkg\n")
		write(t, filepath.Join(root, "instrumentation/lib/kept.go"), "package lib\n")
		write(t, filepath.Join(root, "Makefile"), "trusted\n")
	}
	// The trusted checkout starts with files that the branch deleted or edited.
	write(t, filepath.Join(fx.trusted, "pkg/stale.go"), "stale\n")
	write(t, filepath.Join(fx.trusted, "instrumentation/lib/stale.go"), "stale\n")
	write(t, filepath.Join(fx.trusted, "pkg/sub/kept.go"), "old\n")
	fx.baseSHA = initStageBase(t, fx.trusted)
	return fx
}

// initStageBase makes dir a git checkout and returns its HEAD commit.
func initStageBase(t *testing.T, dir string) string {
	t.Helper()
	gitIn(t, dir, "init", "--quiet", "--initial-branch=main")
	gitIn(t, dir, "config", "user.name", "Test Author")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "--quiet", "--allow-empty", "-m", "base")
	return gitIn(t, dir, "rev-parse", "HEAD")
}

func (fx stageFixture) run(t *testing.T) (int, output) {
	t.Helper()
	return runApp(t, nil, "stage", fx.branch, fx.trusted, fx.baseSHA)
}

// snapshot records every entry below root without following links. The map
// key is the relative path. The value holds the type, the mode, and the content.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	entries := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		value := info.Mode().String()
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, linkErr := os.Readlink(path)
			if linkErr != nil {
				return linkErr
			}
			value += " -> " + target
		case info.Mode().IsRegular():
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			value += " " + string(data)
		}
		entries[rel] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func assertSameTree(t *testing.T, want, got map[string]string, label string) {
	t.Helper()
	if len(want) != len(got) {
		t.Errorf("%s: %d entries, want %d", label, len(got), len(want))
	}
	for path, value := range want {
		if got[path] != value {
			t.Errorf("%s: %s = %q, want %q", label, path, got[path], value)
		}
	}
	for path := range got {
		if _, ok := want[path]; !ok {
			t.Errorf("%s: unexpected entry %s", label, path)
		}
	}
}

// Shell group: test_stage_sync.
func TestStageSync(t *testing.T) {
	fx := newStageFixture(t)
	// Add, edit, and delete files under both trees.
	write(t, filepath.Join(fx.branch, "pkg/sub/added.go"), "package pkg\nvar added int\n")
	write(t, filepath.Join(fx.branch, "pkg/sub/kept.go"), "package pkg\nvar edited int\n")
	write(t, filepath.Join(fx.branch, "instrumentation/new/deep/added.go"), "package deep\n")
	write(t, filepath.Join(fx.branch, "instrumentation/lib/.hidden"), "hidden\n")
	if err := os.MkdirAll(filepath.Join(fx.branch, "pkg/empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(fx.trusted, "pkg/gone.txt"), "data\n")
	// The branch controls these files, but staging must not copy them.
	write(t, filepath.Join(fx.branch, "Makefile"), "hostile\n")
	write(t, filepath.Join(fx.branch, ".tools/bundle/main.go"), "hostile\n")
	write(t, filepath.Join(fx.branch, ".tools/bundlerepair/main.go"), "hostile\n")
	write(t, filepath.Join(fx.branch, "go.mod"), "hostile\n")

	if code, out := fx.run(t); code != 0 {
		t.Fatalf("staging failed with %d: %s", code, out.stderr)
	}
	for _, tree := range []string{"pkg", "instrumentation"} {
		assertSameTree(t, snapshot(t, filepath.Join(fx.branch, tree)), snapshot(t, filepath.Join(fx.trusted, tree)),
			"the trusted "+tree+" tree")
	}
	if got, err := os.ReadFile(filepath.Join(fx.trusted, "Makefile")); err != nil || string(got) != "trusted\n" {
		t.Errorf("staging copied a hostile Makefile: %q, %v", got, err)
	}
	for _, name := range []string{".tools", "go.mod"} {
		if _, err := os.Lstat(filepath.Join(fx.trusted, name)); !os.IsNotExist(err) {
			t.Errorf("staging copied %s outside the bundled trees", name)
		}
	}
	for _, name := range []string{"pkg/stale.go", "pkg/gone.txt", "instrumentation/lib/stale.go"} {
		if _, err := os.Lstat(filepath.Join(fx.trusted, name)); !os.IsNotExist(err) {
			t.Errorf("staging kept %s", name)
		}
	}
}

// TestStagePreservesTree checks the copy of the tree itself. The exact
// permission bits are in stage_unix_test.go, because Windows os.Chmod only
// changes the owner-write bit.
func TestStagePreservesTree(t *testing.T) {
	fx := newStageFixture(t)
	script := filepath.Join(fx.branch, "pkg/sub/run.sh")
	write(t, script, "#!/bin/sh\n")
	readonly := filepath.Join(fx.branch, "pkg/sub/readonly.txt")
	write(t, readonly, "data\n")
	lockedDir := filepath.Join(fx.branch, "instrumentation/locked")
	write(t, filepath.Join(lockedDir, "inner.go"), "package locked\n")
	for path, mode := range map[string]os.FileMode{script: 0o755, readonly: 0o444, lockedDir: 0o555} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.Chmod(lockedDir, 0o755) })
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(fx.trusted, "instrumentation/locked"), 0o755) })

	if code, out := fx.run(t); code != 0 {
		t.Fatalf("staging failed with %d: %s", code, out.stderr)
	}
	for _, tree := range []string{"pkg", "instrumentation"} {
		assertSameTree(t, snapshot(t, filepath.Join(fx.branch, tree)), snapshot(t, filepath.Join(fx.trusted, tree)),
			"the trusted "+tree+" tree")
	}
}

// TestStageRejectsMovedBase checks the fail-closed guard. A trusted checkout
// that is not at the captured base must stop staging before any write.
func TestStageRejectsMovedBase(t *testing.T) {
	fx := newStageFixture(t)
	write(t, filepath.Join(fx.branch, "pkg/sub/added.go"), "package pkg\n")
	before := []map[string]string{
		snapshot(t, filepath.Join(fx.trusted, "pkg")),
		snapshot(t, filepath.Join(fx.trusted, "instrumentation")),
	}
	other := strings.Repeat("0", 40)
	code, out := runApp(t, nil, "stage", fx.branch, fx.trusted, other)
	if code != exitFailure {
		t.Fatalf("exit code = %d, want %d (stderr %q)", code, exitFailure, out.stderr)
	}
	if !strings.Contains(out.stderr, wantBaseMoved) {
		t.Errorf("stderr = %q, want %q", out.stderr, wantBaseMoved)
	}
	for i, tree := range []string{"pkg", "instrumentation"} {
		assertSameTree(t, before[i], snapshot(t, filepath.Join(fx.trusted, tree)),
			"the trusted "+tree+" tree after a moved base")
	}
}

// TestStageRejectsForeignTrustedDir checks a trusted directory that is not a
// git checkout. The guard cannot confirm the base, so staging must stop.
func TestStageRejectsForeignTrustedDir(t *testing.T) {
	fx := newStageFixture(t)
	if err := os.RemoveAll(filepath.Join(fx.trusted, ".git")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(fx.branch, "pkg/sub/added.go"), "package pkg\n")
	before := snapshot(t, filepath.Join(fx.trusted, "pkg"))
	code, out := runApp(t, nil, "stage", fx.branch, fx.trusted, fx.baseSHA)
	if code != exitFailure {
		t.Fatalf("exit code = %d, want %d (stderr %q)", code, exitFailure, out.stderr)
	}
	assertSameTree(t, before, snapshot(t, filepath.Join(fx.trusted, "pkg")),
		"the trusted pkg tree after a non-git trusted directory")
}

func TestStageReplacesTrustedTreeLink(t *testing.T) {
	fx := newStageFixture(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "keep.txt"), "keep\n")
	if err := os.RemoveAll(filepath.Join(fx.trusted, "pkg")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(fx.trusted, "pkg")); err != nil {
		t.Fatal(err)
	}
	if code, out := fx.run(t); code != 0 {
		t.Fatalf("staging failed with %d: %s", code, out.stderr)
	}
	info, err := os.Lstat(filepath.Join(fx.trusted, "pkg"))
	if err != nil || !info.IsDir() {
		t.Errorf("trusted pkg = %v, %v, want a directory", info, err)
	}
	if got, readErr := os.ReadFile(filepath.Join(outside, "keep.txt")); readErr != nil || string(got) != "keep\n" {
		t.Errorf("staging changed the link target: %q, %v", got, readErr)
	}
}

func TestStageMissingTrustedTrees(t *testing.T) {
	fx := stageFixture{branch: t.TempDir(), trusted: t.TempDir()}
	write(t, filepath.Join(fx.branch, "pkg/a.go"), "a\n")
	write(t, filepath.Join(fx.branch, "instrumentation/b.go"), "b\n")
	fx.baseSHA = initStageBase(t, fx.trusted)
	if code, out := fx.run(t); code != 0 {
		t.Fatalf("staging failed with %d: %s", code, out.stderr)
	}
	if _, err := os.Stat(filepath.Join(fx.trusted, "instrumentation/b.go")); err != nil {
		t.Error(err)
	}
}

// Shell group: test_stage_rejections. The unix-only FIFO case is in
// stage_unix_test.go.
func TestStageRejections(t *testing.T) {
	scenarios := map[string]func(t *testing.T, fx stageFixture, tree string){
		"file-link": func(t *testing.T, fx stageFixture, tree string) {
			symlink(t, "/etc/passwd", filepath.Join(fx.branch, tree, "link.go"))
		},
		"dir-link": func(t *testing.T, fx stageFixture, tree string) {
			symlink(t, t.TempDir(), filepath.Join(fx.branch, tree, "dir-link"))
		},
		"dangling-link": func(t *testing.T, fx stageFixture, tree string) {
			symlink(t, "does-not-exist", filepath.Join(fx.branch, tree, "dangling"))
		},
		"nested-link": func(t *testing.T, fx stageFixture, tree string) {
			symlink(t, "../..", filepath.Join(fx.branch, tree, nestedDir(tree), "up"))
		},
		"root-link": func(t *testing.T, fx stageFixture, tree string) {
			elsewhere := filepath.Join(fx.branch, "elsewhere")
			write(t, filepath.Join(elsewhere, "x.go"), "package x\n")
			replace(t, filepath.Join(fx.branch, tree))
			symlink(t, elsewhere, filepath.Join(fx.branch, tree))
		},
		"missing": func(t *testing.T, fx stageFixture, tree string) {
			replace(t, filepath.Join(fx.branch, tree))
		},
		"root-file": func(t *testing.T, fx stageFixture, tree string) {
			replace(t, filepath.Join(fx.branch, tree))
			write(t, filepath.Join(fx.branch, tree), "file\n")
		},
		"other-tree-link": func(t *testing.T, fx stageFixture, tree string) {
			// A bad entry in the other tree must also stop the copy of this one.
			other := "instrumentation"
			if tree == "instrumentation" {
				other = "pkg"
			}
			symlink(t, "/etc/passwd", filepath.Join(fx.branch, other, nestedDir(other), "link.go"))
			write(t, filepath.Join(fx.branch, tree, "changed.go"), "changed\n")
		},
	}
	for _, tree := range []string{"pkg", "instrumentation"} {
		for _, name := range slices.Sorted(maps.Keys(scenarios)) {
			t.Run(tree+"/"+name, func(t *testing.T) {
				fx := newStageFixture(t)
				scenarios[name](t, fx, tree)
				before := snapshot(t, fx.trusted)
				code, out := fx.run(t)
				if code != exitFailure {
					t.Fatalf("exit code = %d, want %d (stderr %q)", code, exitFailure, out.stderr)
				}
				assertSameTree(t, before, snapshot(t, fx.trusted), "the trusted checkout after a rejection")
			})
		}
	}
}

func TestStageRejectionMessages(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, fx stageFixture)
		want  string
	}{
		{"link", func(t *testing.T, fx stageFixture) {
			symlink(t, "x", filepath.Join(fx.branch, "pkg/sub/link.go"))
		}, "is not a regular file or a directory"},
		{"root link", func(t *testing.T, fx stageFixture) {
			replace(t, filepath.Join(fx.branch, "pkg"))
			symlink(t, fx.branch, filepath.Join(fx.branch, "pkg"))
		}, "is a symbolic link"},
		{"missing", func(t *testing.T, fx stageFixture) {
			replace(t, filepath.Join(fx.branch, "instrumentation"))
		}, "is not a directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newStageFixture(t)
			tt.setup(t, fx)
			if _, out := fx.run(t); !strings.Contains(out.stderr, tt.want) {
				t.Errorf("stderr = %q, want %q", out.stderr, tt.want)
			}
		})
	}
}

// Shell group: test_stage_arguments.
func TestStageArguments(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	write(t, file, "x")
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"help", []string{"--help"}, 0},
		{"no arguments", nil, exitFailure},
		{"empty arguments", []string{"", "", ""}, exitFailure},
		{"empty trusted directory", []string{dir, "", testSHA}, exitFailure},
		{"empty base SHA", []string{dir, dir, ""}, exitFailure},
		{"one argument", []string{dir}, exitFailure},
		{"two arguments", []string{dir, dir}, exitFailure},
		{"four arguments", []string{dir, dir, testSHA, testSHA}, exitFailure},
		{"short base SHA", []string{dir, dir, testSHA[:39]}, exitFailure},
		{"uppercase base SHA", []string{dir, dir, strings.ToUpper(testSHA)}, exitFailure},
		{"non-hex base SHA", []string{dir, dir, strings.Repeat("g", 40)}, exitFailure},
		{
			"missing directories",
			[]string{filepath.Join(dir, "missing-a"), filepath.Join(dir, "missing-b"), testSHA},
			exitFailure,
		},
		{"missing trusted directory", []string{dir, filepath.Join(dir, "missing"), testSHA}, exitFailure},
		{"file instead of directory", []string{file, dir, testSHA}, exitFailure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out := runApp(t, nil, append([]string{"stage"}, tt.args...)...)
			if code != tt.want {
				t.Errorf("exit code = %d, want %d", code, tt.want)
			}
			if tt.want == 0 && !strings.Contains(out.stdout, "Usage: bundlerepair stage") {
				t.Errorf("help output = %q", out.stdout)
			}
		})
	}
}

func nestedDir(tree string) string {
	if tree == "pkg" {
		return "sub"
	}
	return "lib"
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func replace(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}
