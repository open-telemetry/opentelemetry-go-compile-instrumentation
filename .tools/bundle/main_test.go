// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShouldExclude(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"instrumentation/redis/.DS_Store", true},
		{".DS_Store", true},
		{"instrumentation/redis/Thumbs.db", true},
		{"instrumentation/redis/desktop.ini", true},
		{"instrumentation/redis/build.log", true},
		{"instrumentation/redis/client.go", false},
		{"instrumentation/redis/go.sum", false},
		// only an exact junk filename is excluded, not anything containing it
		{"instrumentation/redis/not.DS_Store.go", false},
	}

	for _, tt := range tests {
		if got := shouldExclude(tt.name); got != tt.want {
			t.Errorf("shouldExclude(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestArchive_ExcludesOSJunk reproduces the scenario from the bug report: an
// untracked .DS_Store dropped into a source directory by macOS Finder must
// not end up in the produced archive.
func TestArchive_ExcludesOSJunk(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "client.go"), []byte("package redis\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, ".DS_Store"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(t.TempDir(), "out.tgz")
	if err := archive(outPath, []string{srcDir}); err != nil {
		t.Fatalf("archive: %v", err)
	}

	names := readTarNames(t, outPath)
	for _, name := range names {
		if filepath.Base(name) == ".DS_Store" {
			t.Errorf("archive contains excluded junk file %q", name)
		}
	}
	if !containsSuffix(names, "client.go") {
		t.Errorf("archive is missing expected source file, got entries: %v", names)
	}
}

// TestArchive_PrunesJunkOnlyDir verifies a directory containing only OS junk
// is treated as effectively empty and omitted from the archive, matching
// git's behavior for directories with no tracked content.
func TestArchive_PrunesJunkOnlyDir(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "client.go"), []byte("package redis\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	junkOnlyDir := filepath.Join(srcDir, "empty")
	if err := os.Mkdir(junkOnlyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(junkOnlyDir, ".DS_Store"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(t.TempDir(), "out.tgz")
	if err := archive(outPath, []string{srcDir}); err != nil {
		t.Fatalf("archive: %v", err)
	}

	names := readTarNames(t, outPath)
	for _, name := range names {
		if strings.Contains(name, "empty") {
			t.Errorf("archive contains junk-only directory %q, want it pruned", name)
		}
	}
}

func readTarNames(t *testing.T, path string) []string {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()

	var names []string
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		names = append(names, hdr.Name)
	}
	return names
}

func containsSuffix(names []string, suffix string) bool {
	for _, name := range names {
		if filepath.Base(name) == suffix {
			return true
		}
	}
	return false
}
