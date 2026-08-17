// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"io"
	"net"
	"net/http"
)

// Compile-time assertions that writerWrapper satisfies the optional interfaces
// an http.ResponseWriter may implement.
var (
	_ http.ResponseWriter = (*writerWrapper)(nil)
	_ http.Flusher        = (*writerWrapper)(nil)
	_ http.Hijacker       = (*writerWrapper)(nil)
	_ http.Pusher         = (*writerWrapper)(nil)
	_ io.ReaderFrom       = (*writerWrapper)(nil)
	// Required by http.ResponseController to reach the real writer for
	// SetReadDeadline, SetWriteDeadline and EnableFullDuplex.
	_ interface{ Unwrap() http.ResponseWriter } = (*writerWrapper)(nil)
)

// writerWrapper wraps http.ResponseWriter to capture the status code.
//
// This wrapper is substituted for the application's real http.ResponseWriter
// before its handler runs, so it must stay as transparent as possible: any
// capability of the underlying writer that the wrapper fails to forward
// silently disappears from the instrumented application.
//
// Note that the wrapper unconditionally implements Flusher, Hijacker, Pusher
// and ReaderFrom, so a handler that probes with a type assertion always sees
// them present. Each method degrades to http.ErrNotSupported (or a no-op for
// Flush) when the underlying writer lacks the capability, which matches the
// contract those interfaces document for unsupported transports. Handlers that
// need an accurate answer should use http.ResponseController, which follows
// Unwrap down to the real writer.
type writerWrapper struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

// WriteHeader captures the status code and forwards to the underlying ResponseWriter
func (w *writerWrapper) WriteHeader(statusCode int) {
	// Prevent duplicate header writes
	if w.wroteHeader {
		return
	}
	w.statusCode = statusCode
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(statusCode)
}

// Write implements http.ResponseWriter.Write and ensures WriteHeader is called
func (w *writerWrapper) Write(b []byte) (int, error) {
	// If WriteHeader wasn't called yet, call it with 200 OK (default HTTP behavior)
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (w *writerWrapper) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// ReadFrom implements io.ReaderFrom so that io.Copy and http.ServeFile keep the
// underlying writer's sendfile fast path instead of falling back to a buffered
// copy through Write.
//
// Headers are not committed eagerly: if src fails before producing any bytes,
// wroteHeader remains false so the caller can still send an error status code.
func (w *writerWrapper) ReadFrom(src io.Reader) (int64, error) {
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		n, err := rf.ReadFrom(src)
		if n > 0 && !w.wroteHeader {
			w.wroteHeader = true
			if w.statusCode == 0 {
				w.statusCode = http.StatusOK
			}
		}
		return n, err
	}
	// Wrap w in a struct exposing only io.Writer to prevent io.Copy from calling
	// w.ReadFrom recursively, while routing through w.Write which sets
	// wroteHeader and statusCode only after the first successful chunk read.
	return io.Copy(struct{ io.Writer }{w}, src)
}

// Hijack implements the http.Hijacker interface
func (w *writerWrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// FlushError implements the interface http.ResponseController prefers over
// http.Flusher, so a failed flush is reported to the handler instead of being
// silently swallowed.
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

// Flush implements the http.Flusher interface
func (w *writerWrapper) Flush() {
	_ = w.FlushError()
}

// Push implements the http.Pusher interface, forwarding to the underlying
// ResponseWriter when it supports HTTP/2 server push and returning
// http.ErrNotSupported otherwise.
func (w *writerWrapper) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}
