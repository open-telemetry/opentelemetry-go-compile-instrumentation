// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const bundlePath = "tool/data/otelc-bundle.tgz"

// gitIn runs git in dir with the user and system configuration disabled.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitAll(t *testing.T, dir, message string) {
	t.Helper()
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "--quiet", "-m", message)
}

// mergeFixture is a repository with a contribution branch checked out.
type mergeFixture struct {
	dir string
	// base is the head commit of main.
	base string
}

// newMergeFixture mirrors fixture in the old shell test. It leaves the
// "contribution" branch checked out.
func newMergeFixture(t *testing.T) mergeFixture {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "--quiet", "--initial-branch=main")
	gitIn(t, dir, "config", "user.name", "Test Author")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	write(t, filepath.Join(dir, bundlePath), "original\x00bundle")
	write(t, filepath.Join(dir, "source.txt"), "original\n")
	commitAll(t, dir, "initial")
	gitIn(t, dir, "checkout", "--quiet", "-b", "contribution")
	write(t, filepath.Join(dir, bundlePath), "contribution\x00bundle")
	commitAll(t, dir, "contribution")
	gitIn(t, dir, "checkout", "--quiet", "main")
	write(t, filepath.Join(dir, bundlePath), "base\x00bundle")
	write(t, filepath.Join(dir, "base.txt"), "base change\n")
	commitAll(t, dir, "base")
	base := gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "checkout", "--quiet", "contribution")
	return mergeFixture{dir: dir, base: base}
}

func runMerge(t *testing.T, args ...string) (int, output) {
	t.Helper()
	return runApp(t, nil, append([]string{"merge"}, args...)...)
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Shell group: main (merge a bundle-only conflict without losing source changes).
func TestMergeBundleOnlyConflict(t *testing.T) {
	fx := newMergeFixture(t)
	dir, base := fx.dir, fx.base
	head := gitIn(t, dir, "rev-parse", "HEAD")
	code, out := runMerge(t, dir, base)
	if code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	if got := gitIn(t, dir, "rev-parse", "MERGE_HEAD"); got != base {
		t.Errorf("MERGE_HEAD = %s, want the base %s", got, base)
	}
	if got := gitIn(t, dir, "show", ":base.txt"); got != "base change" {
		t.Errorf("the merge lost a base change: %q", got)
	}
	if got := readFile(t, filepath.Join(dir, bundlePath)); !bytes.Equal(got, []byte("contribution\x00bundle")) {
		t.Errorf("the merge changed the bundle: %q", got)
	}
	if got := gitIn(t, dir, "rev-parse", "HEAD"); got != head {
		t.Error("the merge created a commit")
	}
	if got := gitIn(t, dir, "diff", "--name-only", "--diff-filter=U"); got != "" {
		t.Errorf("unmerged files: %q", got)
	}
}

// Shell group: test_source_conflict.
func TestMergeContributorAttributesCannotHideSourceConflict(t *testing.T) {
	dir := newMergeFixture(t).dir
	gitIn(t, dir, "checkout", "--quiet", "main")
	write(t, filepath.Join(dir, "source.txt"), "base source\n")
	gitIn(t, dir, "commit", "--quiet", "-am", "base source")
	base := gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "checkout", "--quiet", "contribution")
	write(t, filepath.Join(dir, "source.txt"), "contributor source\n")
	write(t, filepath.Join(dir, ".gitattributes"), "* merge=otelc-bundle\n")
	commitAll(t, dir, "contributor source and attributes")

	code, out := runMerge(t, dir, base)
	if code != exitFailure {
		t.Fatalf("contributor attributes hid a source conflict: exit code %d\n%s%s", code, out.stdout, out.stderr)
	}
	if got := gitIn(t, dir, "diff", "--name-only", "--diff-filter=U"); got != "source.txt" {
		t.Errorf("unmerged files = %q, want source.txt", got)
	}
	if !strings.Contains(out.stdout, "CONFLICT") {
		t.Errorf("the merge output is not forwarded: %q", out.stdout)
	}
}

// Go test TestArchiveConversionAttributes (the merge half): contributor
// attributes must not convert the archive bytes.
func TestMergePreservesArchiveBytes(t *testing.T) {
	var archive bytes.Buffer
	writer, err := gzip.NewWriterLevel(&archive, gzip.NoCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write([]byte("bundle\r\n$Id: 0123456789012345678901234567890123456789 $\r\n")); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, attr := range []string{"text", "ident", "text eol=crlf", "working-tree-encoding=UTF-16", "filter=hostile", "-merge", "merge=hostile"} {
		t.Run(attr, func(t *testing.T) {
			fx := newMergeFixture(t)
			dir, base := fx.dir, fx.base
			if writeErr := os.WriteFile(filepath.Join(dir, bundlePath), archive.Bytes(), 0o644); writeErr != nil {
				t.Fatal(err)
			}
			commitAll(t, dir, "archive")
			// Commit the attributes last, so that git add does not convert the archive.
			write(t, filepath.Join(dir, ".gitattributes"), bundlePath+" "+attr+"\n")
			gitIn(t, dir, "add", ".gitattributes")
			gitIn(t, dir, "commit", "--quiet", "-m", "attributes")
			if code, out := runMerge(t, dir, base); code != 0 {
				t.Fatalf("exit code = %d: %s", code, out.stderr)
			}
			if got := readFile(t, filepath.Join(dir, bundlePath)); !bytes.Equal(got, archive.Bytes()) {
				t.Error("Git changed the archive bytes in the working tree")
			}
			staged := gitIn(t, dir, "rev-parse", ":"+bundlePath)
			committed := gitIn(t, dir, "rev-parse", "HEAD:"+bundlePath)
			if staged != committed {
				t.Error("the merge changed the staged archive")
			}
		})
	}
}

func TestMergeWritesLocalAttributes(t *testing.T) {
	fx := newMergeFixture(t)
	dir, base := fx.dir, fx.base
	path := filepath.Join(dir, ".git", "info", "attributes")
	write(t, path, "stale content that must not survive\n")
	if code, out := runMerge(t, dir, base); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	want := "* !merge -filter\n" +
		"tool/data/otelc-bundle.tgz merge=otelc-bundle -text -ident !working-tree-encoding !eol\n"
	if got := string(readFile(t, path)); got != want {
		t.Errorf("info/attributes = %q, want %q", got, want)
	}
}

func TestMergeCreatesAttributesDirectory(t *testing.T) {
	fx := newMergeFixture(t)
	dir, base := fx.dir, fx.base
	if err := os.RemoveAll(filepath.Join(dir, ".git", "info")); err != nil {
		t.Fatal(err)
	}
	if code, out := runMerge(t, dir, base); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "info", "attributes")); err != nil {
		t.Error(err)
	}
}

func TestMergeInLinkedWorktree(t *testing.T) {
	fx := newMergeFixture(t)
	dir, base := fx.dir, fx.base
	linked := filepath.Join(t.TempDir(), "linked")
	gitIn(t, dir, "worktree", "add", "--quiet", "-b", "linked", linked, "contribution")
	if code, out := runMerge(t, linked, base); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	if got := gitIn(t, linked, "rev-parse", "MERGE_HEAD"); got != base {
		t.Errorf("MERGE_HEAD = %s, want %s", got, base)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "info", "attributes")); err != nil {
		t.Errorf("the shared attributes file is missing: %v", err)
	}
}

func TestMergeNeverRunsHooks(t *testing.T) {
	fx := newMergeFixture(t)
	dir, base := fx.dir, fx.base
	marker := filepath.Join(t.TempDir(), "hook-ran")
	hook := "#!/bin/sh\ntouch " + marker + "\n"
	for _, name := range []string{"pre-merge-commit", "post-merge", "post-checkout", "prepare-commit-msg", "commit-msg", "pre-commit"} {
		if err := os.WriteFile(filepath.Join(dir, ".git", "hooks", name), []byte(hook), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if code, out := runMerge(t, dir, base); code != 0 {
		t.Fatalf("exit code = %d: %s", code, out.stderr)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("a contributor hook ran during the merge")
	}
}

func TestMergeArguments(t *testing.T) {
	fx := newMergeFixture(t)
	dir, base := fx.dir, fx.base
	notRepo := t.TempDir()
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"help", []string{"--help"}, 0},
		{"no arguments", nil, exitFailure},
		{"one argument", []string{dir}, exitFailure},
		{"empty checkout", []string{"", base}, exitFailure},
		{"three arguments", []string{dir, base, base}, exitFailure},
		{"short SHA", []string{dir, base[:7]}, exitFailure},
		{"upper case SHA", []string{dir, strings.ToUpper(base)}, exitFailure},
		{"branch name", []string{dir, "main"}, exitFailure},
		{"option as SHA", []string{dir, "--abort"}, exitFailure},
		{"SHA with newline", []string{dir, base + "\n"}, exitFailure},
		{"unknown commit", []string{dir, strings.Repeat("a", 40)}, exitFailure},
		{"not a repository", []string{notRepo, base}, exitFailure},
		{"missing directory", []string{filepath.Join(notRepo, "missing"), base}, exitFailure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := ""
			if tt.want != 0 {
				before = gitIn(t, dir, "rev-parse", "HEAD")
			}
			code, _ := runMerge(t, tt.args...)
			if code != tt.want {
				t.Errorf("exit code = %d, want %d", code, tt.want)
			}
			if tt.want != 0 {
				if _, err := os.Stat(filepath.Join(dir, ".git", "MERGE_HEAD")); err == nil {
					t.Error("a rejected request started a merge")
				}
				if got := gitIn(t, dir, "rev-parse", "HEAD"); got != before {
					t.Error("HEAD moved")
				}
			}
		})
	}
}

func TestMergeErrorCarriesGitStderr(t *testing.T) {
	dir := newMergeFixture(t).dir
	code, out := runMerge(t, dir, strings.Repeat("a", 40))
	if code != exitFailure || !strings.Contains(out.stderr, "git") {
		t.Errorf("exit code = %d, stderr = %q", code, out.stderr)
	}
}
