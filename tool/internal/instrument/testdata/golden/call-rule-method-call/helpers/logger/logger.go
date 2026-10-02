// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package logger

// Logger stands in for a third-party logging type, such as zap.Logger.
type Logger struct{}

func (l Logger) Info(msg string) string { return msg }
