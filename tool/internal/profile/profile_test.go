// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package profile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
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

func TestMerge(t *testing.T) {
	dir := t.TempDir()

	// Produce a real PID-stamped heap profile to merge.
	s, err := Start(dir, []Type{Heap})
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if stopErr := s.Stop(); stopErr != nil {
		t.Fatalf("Stop() error: %v", stopErr)
	}
	pidFile := filepath.Join(dir, fmt.Sprintf("otelc-heap-%d.pprof", os.Getpid()))
	assertFileExists(t, pidFile)

	if mergeErr := Merge(context.Background(), dir, []Type{Heap}); mergeErr != nil {
		t.Fatalf("Merge() error: %v", mergeErr)
	}

	// The merged file is written and the PID-stamped input is removed.
	assertFileExists(t, filepath.Join(dir, "otelc-heap.pprof"))
	if _, statErr := os.Stat(pidFile); !os.IsNotExist(statErr) {
		t.Errorf("expected PID-stamped file %q to be removed after merge", pidFile)
	}
}

func TestMergeTraceSkipped(t *testing.T) {
	dir := t.TempDir()

	// Trace profiles cannot be merged, so Merge is a no-op for them and must not
	// create a merged trace file.
	if err := Merge(context.Background(), dir, []Type{Trace}); err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "otelc-trace.pprof")); !os.IsNotExist(statErr) {
		t.Error("Merge() must not create a merged trace file")
	}
}

func TestMergeNoFiles(t *testing.T) {
	// With no matching profile files present, Merge succeeds without writing anything.
	if err := Merge(context.Background(), t.TempDir(), []Type{Heap, CPU}); err != nil {
		t.Fatalf("Merge() error: %v", err)
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

func TestStartCPUCreateFileError(t *testing.T) {
	dir := t.TempDir()
	// A directory occupying the CPU profile path makes os.Create fail.
	path := filepath.Join(dir, fmt.Sprintf("otelc-cpu-%d.pprof", os.Getpid()))
	require.NoError(t, os.Mkdir(path, 0o755))

	_, err := Start(dir, []Type{CPU})
	require.Error(t, err)
	require.ErrorContains(t, err, "create CPU profile")
}

func TestStartCPUProfileAlreadyRunning(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp(t.TempDir(), "cpu")
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, pprof.StartCPUProfile(f))
	defer pprof.StopCPUProfile()

	_, err = Start(dir, []Type{CPU})
	require.Error(t, err)
	require.ErrorContains(t, err, "start CPU profile")
}

func TestStartTraceCreateFileError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, fmt.Sprintf("otelc-%d.trace", os.Getpid()))
	require.NoError(t, os.Mkdir(path, 0o755))

	_, err := Start(dir, []Type{Trace})
	require.Error(t, err)
	require.ErrorContains(t, err, "create trace file")
}

func TestStartTraceAlreadyRunning(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp(t.TempDir(), "trace")
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, trace.Start(f))
	defer trace.Stop()

	_, err = Start(dir, []Type{Trace})
	require.Error(t, err)
	require.ErrorContains(t, err, "start execution trace")
}

func TestStopCPUCloseError(t *testing.T) {
	dir := t.TempDir()
	s, err := Start(dir, []Type{CPU})
	require.NoError(t, err)
	require.NotNil(t, s.cpuFile)
	require.NoError(t, s.cpuFile.Close())

	stopErr := s.Stop()
	require.Error(t, stopErr)
	require.ErrorContains(t, stopErr, "close CPU profile")
}

func TestStopTraceCloseError(t *testing.T) {
	dir := t.TempDir()
	s, err := Start(dir, []Type{Trace})
	require.NoError(t, err)
	require.NotNil(t, s.traceFile)
	require.NoError(t, s.traceFile.Close())

	stopErr := s.Stop()
	require.Error(t, stopErr)
	require.ErrorContains(t, stopErr, "close trace file")
}

func TestWriteHeapProfileCreateError(t *testing.T) {
	s := &Session{dir: t.TempDir()}
	path := filepath.Join(s.dir, fmt.Sprintf("otelc-heap-%d.pprof", os.Getpid()))
	require.NoError(t, os.Mkdir(path, 0o755))

	err := s.writeHeapProfile()
	require.Error(t, err)
	require.ErrorContains(t, err, "create heap profile")
}

func TestMergeTypeGlobError(t *testing.T) {
	// An unclosed bracket in the directory name makes filepath.Glob fail.
	dir := filepath.Join(t.TempDir(), "a[")
	err := mergeType(context.Background(), dir, CPU)
	require.Error(t, err)
}

func TestMergeReturnsMergeError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a[")
	err := Merge(context.Background(), dir, []Type{CPU})
	require.Error(t, err)
}

func TestMergeTypeCreateOutputError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "otelc-cpu-1.pprof"), []byte("data"), 0o644))
	// The merged output path is blocked by a directory.
	require.NoError(t, os.Mkdir(filepath.Join(dir, "otelc-cpu.pprof"), 0o755))

	err := mergeType(context.Background(), dir, CPU)
	require.Error(t, err)
	require.ErrorContains(t, err, "create merged")
}

func TestMergeTypeGoToolFailsWithStderr(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "otelc-cpu-1.pprof"), []byte("data"), 0o644))

	bin := t.TempDir()
	if runtime.GOOS == "windows" {
		script := filepath.Join(bin, "go.bat")
		require.NoError(t, os.WriteFile(script, []byte("@echo merge failed 1>&2\r\nexit /b 1\r\n"), 0o644))
	} else {
		script := filepath.Join(bin, "go")
		require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nprintf 'merge failed\n' 1>&2\nexit 1\n"), 0o755))
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := mergeType(context.Background(), dir, CPU)
	require.Error(t, err)
	require.ErrorContains(t, err, "merge failed")
}

func TestMergeTypeGoToolFailsWithoutStderr(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "otelc-cpu-1.pprof"), []byte("data"), 0o644))

	bin := t.TempDir()
	if runtime.GOOS == "windows" {
		script := filepath.Join(bin, "go.bat")
		require.NoError(t, os.WriteFile(script, []byte("@exit /b 1\r\n"), 0o644))
	} else {
		script := filepath.Join(bin, "go")
		require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755))
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := mergeType(context.Background(), dir, CPU)
	require.Error(t, err)
}

func TestStopHeapWriteError(t *testing.T) {
	dir := t.TempDir()
	s, err := Start(dir, []Type{Heap})
	require.NoError(t, err)
	require.NotNil(t, s)

	path := filepath.Join(dir, fmt.Sprintf("otelc-heap-%d.pprof", os.Getpid()))
	require.NoError(t, os.Mkdir(path, 0o755))

	stopErr := s.Stop()
	require.Error(t, stopErr)
	require.ErrorContains(t, stopErr, "write heap profile")
}

func TestMergeTypeGoToolNotFound(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "otelc-cpu-1.pprof"), []byte("data"), 0o644))
	t.Setenv("PATH", "")

	err := mergeType(context.Background(), dir, CPU)
	require.Error(t, err)
	require.ErrorContains(t, err, "merge cpu profiles")
}
