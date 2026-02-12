package slog

import (
	logslog "log/slog"
)

type Handler logslog.Handler

type Logger struct {
	handler logslog.Handler
}
