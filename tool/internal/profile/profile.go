// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package profile

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/pprof"
	"runtime/trace"
	"slices"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

const (
	// EnvProfilePath is the directory where profile files are written.
	// Set automatically when --profile-path is used; propagated to child processes.
	EnvProfilePath = "OTELC_PROFILE_PATH"

	// EnvEnabledProfiles is a comma-separated list of enabled profile types.
	// Valid values: "cpu", "heap", "trace".
	// Set automatically when --profile is used; propagated to child processes.
	EnvEnabledProfiles = "OTELC_ENABLED_PROFILES"
)

// Type represents a profiling type.
type Type string

const (
	CPU   Type = "cpu"
	Heap  Type = "heap"
	Trace Type = "trace"
)

// Session manages the lifecycle of active profiles for a single process.
// Each otelc process (parent and each toolexec child) gets its own Session.
type Session struct {
	dir       string
	types     []Type
	cpuFile   *os.File
	traceFile *os.File
}

// ParseTypes parses a comma-separated string of profile type names.
// Returns an error if any type name is unrecognized.
// Returns nil, nil for empty input.
func ParseTypes(s string) ([]Type, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	parts := strings.Split(s, ",")
	types := make([]Type, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch Type(p) {
		case CPU, Heap, Trace:
			types = append(types, Type(p))
		default:
			return nil, ex.Newf("unrecognized profile type %q (valid: cpu, heap, trace)", p)
		}
	}
	return types, nil
}

// Start begins profiling and returns a Session. The caller must call Stop when done.
// Each profile file is stamped with the current process PID so parallel
// sub-processes never collide.
func Start(dir string, types []Type) (*Session, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, ex.Newf("create profile directory %q", dir)
	}

	s := &Session{dir: dir, types: types}

	for _, t := range types {
		switch t {
		case CPU:
			path := s.filePath("otelc-cpu-%d.pprof")
			f, err := os.Create(path)
			if err != nil {
				_ = s.Stop()
				return nil, ex.Newf("create CPU profile %q", path)
			}
			if startErr := pprof.StartCPUProfile(f); startErr != nil {
				_ = f.Close()
				_ = os.Remove(path)
				_ = s.Stop()
				return nil, ex.Newf("start CPU profile")
			}
			s.cpuFile = f
		case Trace:
			path := s.filePath("otelc-%d.trace")
			f, err := os.Create(path)
			if err != nil {
				_ = s.Stop()
				return nil, ex.Newf("create trace file %q", path)
			}
			if startErr := trace.Start(f); startErr != nil {
				_ = f.Close()
				_ = os.Remove(path)
				_ = s.Stop()
				return nil, ex.Newf("start execution trace")
			}
			s.traceFile = f
		case Heap:
			// Heap snapshot is taken at Stop time, nothing to start.
		}
	}

	return s, nil
}

// Stop ends all active profiles and writes final snapshots.
// Safe to call on a nil Session (returns nil).
func (s *Session) Stop() error {
	if s == nil {
		return nil
	}

	if s.cpuFile != nil {
		pprof.StopCPUProfile()
		if err := s.cpuFile.Close(); err != nil {
			return ex.Newf("close CPU profile %q", s.cpuFile.Name())
		}
		s.cpuFile = nil
	}

	if s.traceFile != nil {
		trace.Stop()
		if err := s.traceFile.Close(); err != nil {
			return ex.Newf("close trace file %q", s.traceFile.Name())
		}
		s.traceFile = nil
	}

	// Write heap snapshot at the end (captures final allocation state).
	if slices.Contains(s.types, Heap) {
		if err := s.writeHeapProfile(); err != nil {
			return ex.Newf("write heap profile %q", s.filePath("otelc-heap-%d.pprof"))
		}
	}

	return nil
}

// Merge merges all PID-stamped profile files in dir into a single file per type.
// The individual PID-stamped files are removed after a successful merge.
//
// Execution trace files (.trace) are not merged because the Go trace tool
// does not support merging multiple trace files.
//
// Merge requires the Go toolchain to be installed (uses "go tool pprof -proto").
func Merge(ctx context.Context, dir string, types []Type) error {
	for _, t := range types {
		if t == Trace {
			// Execution traces cannot be merged; leave them as-is.
			continue
		}
		if err := mergeType(ctx, dir, t); err != nil {
			return err
		}
	}
	return nil
}

// mergeType merges all PID-stamped files for a single profile type.
func mergeType(ctx context.Context, dir string, t Type) error {
	pattern := filepath.Join(dir, fmt.Sprintf("otelc-%s-*.pprof", t))
	files, err := filepath.Glob(pattern)
	if err != nil {
		return ex.Newf("glob %s profiles", t)
	}
	if len(files) == 0 {
		return nil
	}

	outPath := filepath.Join(dir, fmt.Sprintf("otelc-%s.pprof", t))
	out, err := os.Create(outPath)
	if err != nil {
		return ex.Newf("create merged %s profile %q", t, outPath)
	}

	// "go tool pprof -proto" writes a binary proto-encoded pprof profile to stdout.
	args := append([]string{"tool", "pprof", "-proto"}, files...)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Stdout = out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if runErr := cmd.Run(); runErr != nil {
		_ = out.Close()
		_ = os.Remove(outPath)
		if stderr.Len() > 0 {
			return ex.Newf("merge %s profiles: %s", t, stderr.String())
		}
		return ex.Newf("merge %s profiles", t)
	}

	if closeErr := out.Close(); closeErr != nil {
		_ = os.Remove(outPath)
		return ex.Newf("close merged %s profile", t)
	}

	// Remove individual PID-stamped files now that the merged file is written.
	for _, f := range files {
		_ = os.Remove(f)
	}
	return nil
}

// filePath formats a PID-stamped filename inside the profile directory.
// nameFormat must contain exactly one %d verb for the PID.
func (s *Session) filePath(nameFormat string) string {
	return filepath.Join(s.dir, fmt.Sprintf(nameFormat, os.Getpid()))
}

// writeHeapProfile writes a heap profile snapshot to disk.
func (s *Session) writeHeapProfile() error {
	path := s.filePath("otelc-heap-%d.pprof")
	f, err := os.Create(path)
	if err != nil {
		return ex.Newf("create heap profile %q", path)
	}
	defer f.Close()

	if writeErr := pprof.WriteHeapProfile(f); writeErr != nil {
		_ = os.Remove(path)
		return ex.Newf("write heap profile: %q", path)
	}
	return nil
}
