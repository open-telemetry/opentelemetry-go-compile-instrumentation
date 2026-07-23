// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
)

// Compile-time assertions that writerWrapper preserves the optional interfaces
// a http.ResponseWriter may satisfy. Without these, a wrong method name or
// signature silently drops an interface (see the Push regression these guard
// against), because the embedded http.ResponseWriter only promotes the base
// interface methods.
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
// ResponseWriter when it supports HTTP/2 server push. It returns
// http.ErrNotSupported when the underlying writer is not an http.Pusher, which
// is the sentinel handlers already expect on a non-push connection.
//
// The previous implementation exposed a method named Pusher() http.Pusher,
// which does not match the http.Pusher interface (Push(string, *PushOptions)
// error). As a result the wrapper did not satisfy http.Pusher at all, so a
// handler's `if p, ok := w.(http.Pusher); ok` check silently failed once this
// instrumentation was enabled, disabling HTTP/2 server push.
func (w *writerWrapper) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}
