// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package slog

import (
	"context"
	logslog "log/slog"
)

type HandlerWrapper struct {
	logslog.Handler
	otelHandler logslog.Handler
}

func (w *HandlerWrapper) Enabled(ctx context.Context, level logslog.Level) bool {
	_ = w.otelHandler.Enabled(ctx, level)
	return w.Handler.Enabled(ctx, level)
}

func (w *HandlerWrapper) Handle(ctx context.Context, r logslog.Record) error {
	_ = w.otelHandler.Handle(ctx, r)
	return w.Handler.Handle(ctx, r)
}

func (w *HandlerWrapper) WithAttrs(attrs []logslog.Attr) logslog.Handler {
	return &HandlerWrapper{
		Handler:     w.Handler.WithAttrs(attrs),
		otelHandler: w.otelHandler.WithAttrs(attrs),
	}
}

func (w *HandlerWrapper) WithGroup(name string) logslog.Handler {
	return &HandlerWrapper{
		Handler:     w.Handler.WithGroup(name),
		otelHandler: w.otelHandler.WithGroup(name),
	}
}
