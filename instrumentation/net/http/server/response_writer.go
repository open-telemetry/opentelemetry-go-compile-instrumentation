// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
)

// Compile-time assertions that writerWrapper satisfies the optional interfaces
// an http.ResponseWriter may implement.
var (
	_ http.ResponseWriter = (*writerWrapper)(nil)
	_ http.Hijacker       = (*writerWrapper)(nil)
	_ http.Flusher        = (*writerWrapper)(nil)
	_ http.Pusher         = (*writerWrapper)(nil)
)

// writerWrapper wraps http.ResponseWriter to capture the status code
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

// Hijack implements the http.Hijacker interface
func (w *writerWrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("responseWriter does not implement http.Hijacker")
}

// Flush implements the http.Flusher interface
func (w *writerWrapper) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
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

// Unwrap exposes the underlying writer to http.ResponseController.
func (w *writerWrapper) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
