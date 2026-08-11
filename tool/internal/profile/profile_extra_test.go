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
	"testing"

	"github.com/stretchr/testify/require"
)

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
