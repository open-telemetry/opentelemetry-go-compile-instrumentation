// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package logger

import "context"

// Logger stands in for a third-party logging type, such as zap.Logger.
type Logger struct{}

// With returns a logger that adds the fields of ctx to every entry.
func (l *Logger) With(ctx context.Context) *Logger { return l }

// Info logs msg.
func (l *Logger) Info(msg string) {}

// Log logs msg and returns it.
func Log(msg string) string { return msg }

// LogContext logs msg with the fields of ctx and returns it.
func LogContext(ctx context.Context, msg string) string { return msg }

// Name returns msg.
func Name(msg string) string { return msg }

// Named returns msg prefixed with fn.
func Named(fn, msg string) string { return fn + ": " + msg }
