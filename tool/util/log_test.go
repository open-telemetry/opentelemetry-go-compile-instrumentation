// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestContextLogger(t *testing.T) {
	t.Run("round-trips a stored logger", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		ctx := ContextWithLogger(context.Background(), logger)
		assert.Same(t, logger, LoggerFromContext(ctx))
	})

	t.Run("returns default logger when absent", func(t *testing.T) {
		assert.Same(t, slog.Default(), LoggerFromContext(context.Background()))
	})
}
