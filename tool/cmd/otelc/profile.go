// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

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

	"go.opentelemetry.io/otelc/tool/ex"
)

const (
	// envProfilePath is the directory where profile files are written.
	// Set automatically when --profile-path is used; propagated to child processes.
	envProfilePath = "OTELC_PROFILE_PATH"

	// envEnabledProfiles is a comma-separated list of enabled profile types.
	// Valid values: "cpu", "heap", "trace".
	// Set automatically when --profile is used; propagated to child processes.
	envEnabledProfiles = "OTELC_ENABLED_PROFILES"
)

// profileType represents a profiling type.
type profileType string

const (
	profileTypeCPU   profileType = "cpu"
	profileTypeHeap  profileType = "heap"
	profileTypeTrace profileType = "trace"
)

// profileSession manages the lifecycle of active profiles for a single process.
// Each otelc process (parent and each toolexec child) gets its own profileSession.
type profileSession struct {
	dir       string
	types     []profileType
	cpuFile   *os.File
	traceFile *os.File
}

// parseProfileTypes parses a comma-separated string of profile type names.
// Returns an error if any type name is unrecognized.
// Returns nil, nil for empty input.
func parseProfileTypes(s string) ([]profileType, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	parts := strings.Split(s, ",")
	types := make([]profileType, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch profileType(p) {
		case profileTypeCPU, profileTypeHeap, profileTypeTrace:
			types = append(types, profileType(p))
		default:
			return nil, ex.Newf("unrecognized profile type %q (valid: cpu, heap, trace)", p)
		}
	}
	return types, nil
}

// startProfileSession begins profiling and returns a profileSession. The caller must call stop when done.
// Each profile file is stamped with the current process PID so parallel
// sub-processes never collide.
func startProfileSession(dir string, types []profileType) (*profileSession, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, ex.Wrapf(err, "create profile directory %q", dir)
	}

	s := &profileSession{dir: dir, types: types}

	for _, t := range types {
		switch t {
		case profileTypeCPU:
			path := s.filePath("otelc-cpu-%d.pprof")
			f, err := os.Create(path)
			if err != nil {
				_ = s.stop()
				return nil, ex.Wrapf(err, "create CPU profile %q", path)
			}
			if startErr := pprof.StartCPUProfile(f); startErr != nil {
				_ = f.Close()
				_ = os.Remove(path)
				_ = s.stop()
				return nil, ex.Wrapf(startErr, "start CPU profile")
			}
			s.cpuFile = f
		case profileTypeTrace:
			path := s.filePath("otelc-%d.trace")
			f, err := os.Create(path)
			if err != nil {
				_ = s.stop()
				return nil, ex.Wrapf(err, "create trace file %q", path)
			}
			if startErr := trace.Start(f); startErr != nil {
				_ = f.Close()
				_ = os.Remove(path)
				_ = s.stop()
				return nil, ex.Wrapf(startErr, "start execution trace")
			}
			s.traceFile = f
		case profileTypeHeap:
			// Heap snapshot is taken at stop time, nothing to start.
		}
	}

	return s, nil
}

// stop ends all active profiles and writes final snapshots.
// Safe to call on a nil profileSession (returns nil).
func (s *profileSession) stop() error {
	if s == nil {
		return nil
	}
	var errs []error

	if s.cpuFile != nil {
		pprof.StopCPUProfile()
		if err := s.cpuFile.Close(); err != nil {
			errs = append(errs, ex.Wrapf(err, "close CPU profile %q", s.cpuFile.Name()))
		}
		s.cpuFile = nil
	}

	if s.traceFile != nil {
		trace.Stop()
		if err := s.traceFile.Close(); err != nil {
			errs = append(errs, ex.Wrapf(err, "close trace file %q", s.traceFile.Name()))
		}
		s.traceFile = nil
	}

	// Write heap snapshot at the end (captures final allocation state).
	if slices.Contains(s.types, profileTypeHeap) {
		if err := s.writeHeapProfile(); err != nil {
			errs = append(errs, ex.Wrapf(err, "write heap profile %q", s.filePath("otelc-heap-%d.pprof")))
		}
	}

	return ex.Join(errs...)
}

// mergeProfiles merges all PID-stamped profile files in dir into a single file per type.
// The individual PID-stamped files are removed after a successful merge.
//
// Execution trace files (.trace) are not merged because the Go trace tool
// does not support merging multiple trace files.
//
// mergeProfiles requires the Go toolchain to be installed (uses "go tool pprof -proto").
func mergeProfiles(ctx context.Context, dir string, types []profileType) error {
	var errs []error
	for _, t := range types {
		if t == profileTypeTrace {
			// Execution traces cannot be merged; leave them as-is.
			continue
		}
		if err := mergeProfileType(ctx, dir, t); err != nil {
			errs = append(errs, err)
		}
	}
	return ex.Join(errs...)
}

// mergeProfileType merges all PID-stamped files for a single profile type.
func mergeProfileType(ctx context.Context, dir string, t profileType) error {
	pattern := filepath.Join(dir, fmt.Sprintf("otelc-%s-*.pprof", t))
	files, err := filepath.Glob(pattern)
	if err != nil {
		return ex.Wrapf(err, "glob %s profiles", t)
	}
	if len(files) == 0 {
		return nil
	}

	outPath := filepath.Join(dir, fmt.Sprintf("otelc-%s.pprof", t))
	out, err := os.Create(outPath) //nolint:gosec // outPath is inside the user's own --profile-path dir
	if err != nil {
		return ex.Wrapf(err, "create merged %s profile %q", t, outPath)
	}

	// "go tool pprof -proto" writes a binary proto-encoded pprof profile to stdout.
	args := append([]string{"tool", "pprof", "-proto"}, files...)
	cmd := exec.CommandContext(ctx, "go", args...) //nolint:gosec // fixed "go" binary, file args, no shell
	cmd.Stdout = out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if runErr := cmd.Run(); runErr != nil {
		_ = out.Close()
		_ = os.Remove(outPath) //nolint:gosec // outPath is inside the user's own --profile-path dir
		if stderr.Len() > 0 {
			return ex.Newf("merge %s profiles: %s", t, stderr.String())
		}
		return ex.Newf("merge %s profiles", t)
	}

	if closeErr := out.Close(); closeErr != nil {
		_ = os.Remove(outPath) //nolint:gosec // outPath is inside the user's own --profile-path dir
		return ex.Wrapf(closeErr, "close merged %s profile", t)
	}

	// Remove individual PID-stamped files now that the merged file is written.
	for _, f := range files {
		_ = os.Remove(f) //nolint:gosec // f is a profile file found in the user's --profile-path dir
	}
	return nil
}

// filePath formats a PID-stamped filename inside the profile directory.
// nameFormat must contain exactly one %d verb for the PID.
func (s *profileSession) filePath(nameFormat string) string {
	return filepath.Join(s.dir, fmt.Sprintf(nameFormat, os.Getpid()))
}

// writeHeapProfile writes a heap profile snapshot to disk.
func (s *profileSession) writeHeapProfile() error {
	path := s.filePath("otelc-heap-%d.pprof")
	f, err := os.Create(path)
	if err != nil {
		return ex.Wrapf(err, "create heap profile %q", path)
	}

	if writeErr := pprof.WriteHeapProfile(f); writeErr != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return ex.Wrapf(writeErr, "write heap profile: %q", path)
	}

	if closeErr := f.Close(); closeErr != nil {
		_ = os.Remove(path)
		return ex.Wrapf(closeErr, "close heap profile %q", path)
	}
	return nil
}
