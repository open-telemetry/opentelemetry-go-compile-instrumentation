// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package logger

// Field stands in for a structured logging field, such as zap.Field.
type Field struct {
	Key   string
	Value string
}

// String returns a Field.
func String(key, value string) Field { return Field{Key: key, Value: value} }

// Logger stands in for a third-party logging type, such as zap.Logger.
type Logger struct {
	fields []Field
}

// With returns a copy of l that adds fields to every entry.
func (l *Logger) With(fields ...Field) *Logger {
	return &Logger{fields: append(append([]Field(nil), l.fields...), fields...)}
}

// Info logs msg with fields.
func (l *Logger) Info(msg string, fields ...Field) {}

// Sync flushes buffered entries.
func (l *Logger) Sync() error { return nil }

// Flush calls l.Sync.
func Flush(l *Logger) error { return l.Sync() }
