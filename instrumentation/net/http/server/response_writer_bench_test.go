// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// preFixWrapper reproduces writerWrapper as it was before this fix: it wraps
// http.ResponseWriter and forwards Write/WriteHeader, but implements no
// io.ReaderFrom. That's what hides net/http's sendfile(2)/TransmitFile fast
// path from io.Copy and forces the 32KiB user-space copy loop, the
// regression this PR fixes. Kept here only so the benchmark can show the
// before/after difference; do not use it outside this file.
type preFixWrapper struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (w *preFixWrapper) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.statusCode = statusCode
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *preFixWrapper) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

type wrapMode int

const (
	unwrapped wrapMode = iota
	wrappedPreFix
	wrappedFixed
)

func benchmarkServeFile(b *testing.B, mode wrapMode) {
	content := strings.Repeat("otelc", 10_000_000) // ~50MB, large enough for the copy-loop overhead to show over loopback
	dir := b.TempDir()
	path := filepath.Join(dir, "payload.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		b.Fatal(err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case wrappedPreFix:
			w = &preFixWrapper{ResponseWriter: w, statusCode: http.StatusOK}
		case wrappedFixed:
			w = &writerWrapper{ResponseWriter: w, statusCode: http.StatusOK}
		}
		http.ServeFile(w, r, path)
	}))
	defer ts.Close()

	client := ts.Client()

	b.ResetTimer()
	b.SetBytes(int64(len(content)))
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(ts.URL)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			b.Fatal(err)
		}
		resp.Body.Close()
	}
}

// BenchmarkServeFile_Unwrapped measures http.ServeFile against a bare
// net/http ResponseWriter, which reaches sendfile(2)/TransmitFile via
// io.ReaderFrom. This is the ceiling: no wrapper in the way at all.
func BenchmarkServeFile_Unwrapped(b *testing.B) {
	benchmarkServeFile(b, unwrapped)
}

// BenchmarkServeFile_WrappedPreFix measures http.ServeFile through
// preFixWrapper, which reproduces writerWrapper as it was before this PR:
// no io.ReaderFrom, so io.Copy falls back to the 32KiB copy loop. This is
// the regression the PR fixes.
func BenchmarkServeFile_WrappedPreFix(b *testing.B) {
	benchmarkServeFile(b, wrappedPreFix)
}

// BenchmarkServeFile_WrappedFixed measures http.ServeFile through the
// current writerWrapper, which forwards to the underlying io.ReaderFrom
// (see ReadFrom in response_writer.go). Compare against
// BenchmarkServeFile_WrappedPreFix to see the fast path restored, and
// against BenchmarkServeFile_Unwrapped to see it costs nothing extra.
func BenchmarkServeFile_WrappedFixed(b *testing.B) {
	benchmarkServeFile(b, wrappedFixed)
}
