// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoggerFromContext_Default(t *testing.T) {
	ctx := context.Background()

	logger := LoggerFromContext(ctx)

	assert.Equal(t, slog.Default(), logger)
}

func TestContextWithLogger_RoundTrip(t *testing.T) {
	ctx := context.Background()
	customLogger := slog.New(slog.NewTextHandler(nil, nil))

	ctx = ContextWithLogger(ctx, customLogger)

	logger := LoggerFromContext(ctx)
	assert.Equal(t, customLogger, logger)
}

func TestLoggerFromContext_IgnoresWrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), contextKeyLogger{}, "not a logger")

	logger := LoggerFromContext(ctx)

	assert.Equal(t, slog.Default(), logger)
}
