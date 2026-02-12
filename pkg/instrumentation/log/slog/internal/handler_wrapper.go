package slog

func (l *Logger) WrapHandler(wrapperFunc func(Handler) Handler) {
	l.handler = wrapperFunc(l.handler)
}
