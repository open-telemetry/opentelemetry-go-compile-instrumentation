// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package slog

func (l *Logger) WrapHandler(wrapperFunc func(Handler) Handler) {
	l.handler = wrapperFunc(l.handler)
}
