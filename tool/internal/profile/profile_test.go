// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package profile

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseTypes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []Type
		wantErr string
	}{
		{
			name:  "single cpu",
			input: "cpu",
			want:  []Type{CPU},
		},
		{
			name:  "single heap",
			input: "heap",
			want:  []Type{Heap},
		},
		{
			name:  "single trace",
			input: "trace",
			want:  []Type{Trace},
		},
		{
			name:  "all three",
			input: "cpu,heap,trace",
			want:  []Type{CPU, Heap, Trace},
		},
		{
			name:  "spaces around entries trimmed",
			input: "cpu, heap",
			want:  []Type{CPU, Heap},
		},
		{
			name:  "leading and trailing whitespace",
			input: "  cpu,heap  ",
			want:  []Type{CPU, Heap},
		},
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "whitespace only",
			input: "   ",
			want:  nil,
		},
		{
			name:    "unknown type",
			input:   "goroutine",
			wantErr: "unrecognized",
		},
		{
			name:    "mixed valid and invalid",
			input:   "cpu,invalid",
			wantErr: "unrecognized",
		},
		{
			name:    "pprof builtin not accepted",
			input:   "allocs",
			wantErr: "unrecognized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTypes(tt.input)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseTypes(%q) = nil error, want error containing %q", tt.input, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseTypes(%q) error = %q, want it to contain %q", tt.input, err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseTypes(%q) unexpected error: %v", tt.input, err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseTypes(%q) mismatch (-want +got):\n%s", tt.input, diff)
			}
		})
	}
}

func TestStartStopCPU(t *testing.T) {
	dir := t.TempDir()

	s, err := Start(dir, []Type{CPU})
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if stopErr := s.Stop(); stopErr != nil {
		t.Fatalf("Stop() error: %v", stopErr)
	}

	path := filepath.Join(dir, fmt.Sprintf("otelc-cpu-%d.pprof", os.Getpid()))
	assertFileExists(t, path)
}

func TestStartStopHeap(t *testing.T) {
	dir := t.TempDir()

	s, err := Start(dir, []Type{Heap})
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if stopErr := s.Stop(); stopErr != nil {
		t.Fatalf("Stop() error: %v", stopErr)
	}

	path := filepath.Join(dir, fmt.Sprintf("otelc-heap-%d.pprof", os.Getpid()))
	assertFileExists(t, path)
}

func TestStartStopTrace(t *testing.T) {
	dir := t.TempDir()

	s, err := Start(dir, []Type{Trace})
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if stopErr := s.Stop(); stopErr != nil {
		t.Fatalf("Stop() error: %v", stopErr)
	}

	path := filepath.Join(dir, fmt.Sprintf("otelc-%d.trace", os.Getpid()))
	assertFileExists(t, path)
}

func TestStartStopAll(t *testing.T) {
	dir := t.TempDir()
	pid := os.Getpid()

	s, err := Start(dir, []Type{CPU, Heap, Trace})
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if stopErr := s.Stop(); stopErr != nil {
		t.Fatalf("Stop() error: %v", stopErr)
	}

	assertFileExists(t, filepath.Join(dir, fmt.Sprintf("otelc-cpu-%d.pprof", pid)))
	assertFileExists(t, filepath.Join(dir, fmt.Sprintf("otelc-heap-%d.pprof", pid)))
	assertFileExists(t, filepath.Join(dir, fmt.Sprintf("otelc-%d.trace", pid)))
}

func TestStopNilSession(t *testing.T) {
	var s *Session
	if err := s.Stop(); err != nil {
		t.Errorf("Stop() on nil session returned error: %v", err)
	}
}

func TestStartCreatesDirectory(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "profile", "dir")

	s, err := Start(dir, []Type{Heap})
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() {
		if stopErr := s.Stop(); stopErr != nil {
			t.Errorf("Stop() cleanup error: %v", stopErr)
		}
	})

	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		t.Errorf("Start() did not create directory %q", dir)
	}
}

func TestStartInvalidDir(t *testing.T) {
	// Create a regular file, then try to use it as a directory — MkdirAll fails on all platforms.
	f, createErr := os.CreateTemp(t.TempDir(), "not-a-dir")
	if createErr != nil {
		t.Fatalf("create temp file: %v", createErr)
	}
	_ = f.Close()

	_, err := Start(filepath.Join(f.Name(), "subdir"), []Type{Heap})
	if err == nil {
		t.Fatal("Start() with invalid dir returned nil error, want error")
	}
}

// assertFileExists fails the test if the file does not exist or is empty.
func assertFileExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		t.Errorf("expected file %q to exist, but it does not", path)
		return
	}
	if err != nil {
		t.Errorf("stat %q: %v", path, err)
		return
	}
	if info.Size() == 0 {
		t.Errorf("expected file %q to be non-empty", path)
	}
}

func TestMerge(t *testing.T) {
	dir := t.TempDir()
	pid := os.Getpid()

	// 1. Generate a valid CPU profile
	s, err := Start(dir, []Type{CPU})
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if stopErr := s.Stop(); stopErr != nil {
		t.Fatalf("Stop() error: %v", stopErr)
	}

	origPath := filepath.Join(dir, fmt.Sprintf("otelc-cpu-%d.pprof", pid))
	assertFileExists(t, origPath)

	// 2. Duplicate it to simulate multiple process runs
	dupPath := filepath.Join(dir, "otelc-cpu-99999.pprof")
	srcFile, err := os.Open(origPath)
	if err != nil {
		t.Fatalf("open original profile: %v", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(dupPath)
	if err != nil {
		t.Fatalf("create duplicate profile: %v", err)
	}
	defer destFile.Close()

	if _, copyErr := io.Copy(destFile, srcFile); copyErr != nil {
		t.Fatalf("copy profile content: %v", copyErr)
	}
	_ = destFile.Sync()
	_ = destFile.Close()
	_ = srcFile.Close()

	// 3. Merge them
	ctx := context.Background()
	if mergeErr := Merge(ctx, dir, []Type{CPU}); mergeErr != nil {
		t.Fatalf("Merge() error: %v", mergeErr)
	}

	// 4. Verify merged file exists
	mergedPath := filepath.Join(dir, "otelc-cpu.pprof")
	assertFileExists(t, mergedPath)

	// 5. Verify originals are deleted
	if _, statErr1 := os.Stat(origPath); !os.IsNotExist(statErr1) {
		t.Errorf("expected original file %q to be deleted, but it exists", origPath)
	}
	if _, statErr2 := os.Stat(dupPath); !os.IsNotExist(statErr2) {
		t.Errorf("expected duplicate file %q to be deleted, but it exists", dupPath)
	}
}

func TestMergeTraceIgnored(t *testing.T) {
	dir := t.TempDir()
	pid := os.Getpid()

	s, err := Start(dir, []Type{Trace})
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if stopErr := s.Stop(); stopErr != nil {
		t.Fatalf("Stop() error: %v", stopErr)
	}

	origPath := filepath.Join(dir, fmt.Sprintf("otelc-%d.trace", pid))
	assertFileExists(t, origPath)

	ctx := context.Background()
	if mergeErr := Merge(ctx, dir, []Type{Trace}); mergeErr != nil {
		t.Fatalf("Merge() error: %v", mergeErr)
	}

	// Trace files should NOT be merged/deleted
	assertFileExists(t, origPath)
	mergedPath := filepath.Join(dir, "otelc.trace")
	if _, statErr := os.Stat(mergedPath); !os.IsNotExist(statErr) {
		t.Errorf("expected merged trace file to not exist, but it does")
	}
}
