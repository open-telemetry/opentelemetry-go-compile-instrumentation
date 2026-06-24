// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
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

// Pusher implements the http.Pusher interface
func (w *writerWrapper) Pusher() http.Pusher {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher
	}
	return nil
}
