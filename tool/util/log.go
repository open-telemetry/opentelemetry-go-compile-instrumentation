// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"io"
	"log/slog"
)

type (
	contextKeyLogger    struct{}
	contextKeyLogWriter struct{}
)

func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKeyLogger{}, logger)
}

func ContextWithLogWriter(ctx context.Context, writer io.Closer) context.Context {
	return context.WithValue(ctx, contextKeyLogWriter{}, writer)
}

func LoggerFromContext(ctx context.Context) *slog.Logger {
	logger, ok := ctx.Value(contextKeyLogger{}).(*slog.Logger)
	if !ok {
		return slog.Default()
	}
	return logger
}

func LogWriterFromContext(ctx context.Context) io.Closer {
	writer, ok := ctx.Value(contextKeyLogWriter{}).(io.Closer)
	if !ok {
		return nil
	}
	return writer
}
