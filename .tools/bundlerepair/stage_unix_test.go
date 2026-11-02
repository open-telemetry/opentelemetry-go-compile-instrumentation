// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestStagePreservesExactModes checks the exact permission bits. Windows
// os.Chmod only changes the owner-write bit, so this check is unix-only.
func TestStagePreservesExactModes(t *testing.T) {
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
	for rel, want := range map[string]os.FileMode{
		"pkg/sub/run.sh":         0o755,
		"pkg/sub/readonly.txt":   0o444,
		"instrumentation/locked": 0o555,
	} {
		info, err := os.Lstat(filepath.Join(fx.trusted, rel))
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("%s mode = %v, %v, want %v", rel, info, err, want)
		}
	}
}

// Shell group: test_stage_rejections (the fifo scenario).
func TestStageRejectsFIFO(t *testing.T) {
	for _, tree := range []string{"pkg", "instrumentation"} {
		t.Run(tree, func(t *testing.T) {
			fx := newStageFixture(t)
			if err := syscall.Mkfifo(filepath.Join(fx.branch, tree, "pipe"), 0o600); err != nil {
				t.Fatal(err)
			}
			before := snapshot(t, fx.trusted)
			code, out := fx.run(t)
			if code != exitFailure {
				t.Fatalf("exit code = %d, want %d (stderr %q)", code, exitFailure, out.stderr)
			}
			assertSameTree(t, before, snapshot(t, fx.trusted), "the trusted checkout after a rejection")
		})
	}
}
