// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package main

import (
	"path/filepath"
	"syscall"
	"testing"
)

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
