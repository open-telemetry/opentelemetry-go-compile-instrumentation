// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package slog

import (
	logslog "log/slog"
)

type Handler logslog.Handler

type Logger struct {
	handler logslog.Handler
}
