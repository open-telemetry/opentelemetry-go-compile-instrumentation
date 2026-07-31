// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"testing"
)

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func TestLogWriterFromContext(t *testing.T) {
	t.Run("returns nil when not set", func(t *testing.T) {
		if got := LogWriterFromContext(context.Background()); got != nil {
			t.Errorf("expected nil writer, got %v", got)
		}
	})

	t.Run("round-trips the stored writer", func(t *testing.T) {
		closed := false
		writer := closerFunc(func() error {
			closed = true
			return nil
		})

		ctx := ContextWithLogWriter(context.Background(), writer)
		got := LogWriterFromContext(ctx)
		if got == nil {
			t.Fatal("expected writer from context, got nil")
		}
		if err := got.Close(); err != nil {
			t.Fatalf("Close() returned error: %v", err)
		}
		if !closed {
			t.Error("expected stored writer to be closed")
		}
	})
}
