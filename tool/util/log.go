// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"log/slog"
)

type contextKeyLogger struct{}

func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKeyLogger{}, logger)
}

func LoggerFromContext(ctx context.Context) *slog.Logger {
	logger, ok := ctx.Value(contextKeyLogger{}).(*slog.Logger)
	if !ok {
		return slog.Default()
	}
	return logger
}
