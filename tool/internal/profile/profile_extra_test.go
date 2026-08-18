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

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

func init() {
	mode := os.Getenv("TEST_MOCK_GO_TOOL")
	base := strings.ToLower(filepath.Base(os.Args[0]))
	if mode != "" || base == "go" || base == "go.exe" {
		if mode == "stderr" {
			_, _ = fmt.Fprintln(os.Stderr, "merge failed")
		}
		os.Exit(1)
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

func TestMergeTypeReadDirError(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0o644))
	err := mergeType(context.Background(), filePath, CPU)
	require.Error(t, err)
	require.ErrorContains(t, err, "read cpu profile directory")
}

func TestMergeReturnsMergeError(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0o644))
	err := Merge(context.Background(), filePath, []Type{CPU})
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
	exe, err := os.Executable()
	require.NoError(t, err)

	name := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	mockGo := filepath.Join(bin, name)
	require.NoError(t, util.CopyFile(exe, mockGo))

	t.Setenv("TEST_MOCK_GO_TOOL", "stderr")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	err = mergeType(context.Background(), dir, CPU)
	require.Error(t, err)
	require.ErrorContains(t, err, "merge failed")
}

func TestMergeTypeGoToolFailsWithoutStderr(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "otelc-cpu-1.pprof"), []byte("data"), 0o644))

	bin := t.TempDir()
	exe, err := os.Executable()
	require.NoError(t, err)

	name := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	mockGo := filepath.Join(bin, name)
	require.NoError(t, util.CopyFile(exe, mockGo))

	t.Setenv("TEST_MOCK_GO_TOOL", "silent")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	err = mergeType(context.Background(), dir, CPU)
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
