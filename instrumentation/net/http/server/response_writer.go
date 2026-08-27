// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"net"
	"net/http"
)

// Compile-time interface assertions.
var (
	_ http.ResponseWriter = (*writerWrapper)(nil)
	_ http.Flusher        = (*writerWrapper)(nil)
	_ http.Hijacker       = (*writerWrapper)(nil)
	_ http.Pusher         = (*writerWrapper)(nil)
	_ interface{ Unwrap() http.ResponseWriter } = (*writerWrapper)(nil)
)

// writerWrapper wraps http.ResponseWriter to capture the status code.
// Optional interfaces (Flusher, Hijacker, Pusher) are forwarded to the
// underlying writer; each returns http.ErrNotSupported when unavailable.
type writerWrapper struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (w *writerWrapper) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.statusCode = statusCode
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *writerWrapper) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *writerWrapper) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *writerWrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// FlushError is preferred by http.ResponseController over http.Flusher.
func (w *writerWrapper) FlushError() error {
	if fe, ok := w.ResponseWriter.(interface{ FlushError() error }); ok {
		return fe.FlushError()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
		return nil
	}
	return http.ErrNotSupported
}

func (w *writerWrapper) Flush() { _ = w.FlushError() }

func (w *writerWrapper) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}
