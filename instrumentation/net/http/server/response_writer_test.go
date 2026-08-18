// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriterWrapper_WriteHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &writerWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	// Test writing status code
	wrapper.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, wrapper.statusCode)
	assert.Equal(t, http.StatusCreated, recorder.Code)
}

func TestWriterWrapper_WriteHeader_Default(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &writerWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	// Default status code should be 200
	assert.Equal(t, http.StatusOK, wrapper.statusCode)
}

func TestWriterWrapper_WriteHeader_PreventDuplicate(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &writerWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	// First write
	wrapper.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, wrapper.statusCode)

	// Second write should not change the status code
	wrapper.WriteHeader(http.StatusBadRequest)
	assert.Equal(t, http.StatusCreated, wrapper.statusCode)
}

func TestWriterWrapper_Write(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &writerWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	data := []byte("test data")
	n, err := wrapper.Write(data)

	require.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, "test data", recorder.Body.String())
}

func TestWriterWrapper_Header(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &writerWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	wrapper.Header().Set("Content-Type", "application/json")
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
}

// mockHijacker is a mock ResponseWriter that implements the Hijacker interface
type mockHijacker struct {
	http.ResponseWriter
	hijackCalled bool
}

func (m *mockHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	m.hijackCalled = true
	return nil, nil, nil
}

func TestWriterWrapper_Hijack(t *testing.T) {
	mock := &mockHijacker{ResponseWriter: httptest.NewRecorder()}
	wrapper := &writerWrapper{
		ResponseWriter: mock,
		statusCode:     http.StatusOK,
	}

	conn, rw, err := wrapper.Hijack()
	require.NoError(t, err)
	assert.Nil(t, conn)
	assert.Nil(t, rw)
	assert.True(t, mock.hijackCalled)
}

func TestWriterWrapper_Hijack_NotSupported(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &writerWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	conn, rw, err := wrapper.Hijack()
	require.Error(t, err)
	assert.Nil(t, conn)
	assert.Nil(t, rw)
	assert.Contains(t, err.Error(), "does not implement http.Hijacker")
}

// mockFlusher is a mock ResponseWriter that implements the Flusher interface
type mockFlusher struct {
	http.ResponseWriter
	flushCalled bool
}

func (m *mockFlusher) Flush() {
	m.flushCalled = true
}

func TestWriterWrapper_Flush(t *testing.T) {
	mock := &mockFlusher{ResponseWriter: httptest.NewRecorder()}
	wrapper := &writerWrapper{
		ResponseWriter: mock,
		statusCode:     http.StatusOK,
	}

	wrapper.Flush()
	assert.True(t, mock.flushCalled)
}

func TestWriterWrapper_Flush_NotSupported(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &writerWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	// Should not panic when Flush is not supported
	wrapper.Flush()
}

// mockPusher is a mock ResponseWriter that implements the Pusher interface
type mockPusher struct {
	http.ResponseWriter
	pushCalled bool
}

func (m *mockPusher) Push(target string, opts *http.PushOptions) error {
	m.pushCalled = true
	return nil
}

func TestWriterWrapper_Push(t *testing.T) {
	mock := &mockPusher{ResponseWriter: httptest.NewRecorder()}
	wrapper := &writerWrapper{
		ResponseWriter: mock,
		statusCode:     http.StatusOK,
	}

	err := wrapper.Push("/test", nil)
	require.NoError(t, err)
	assert.True(t, mock.pushCalled)
}

func TestWriterWrapper_Push_NotSupported(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &writerWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	err := wrapper.Push("/test", nil)
	require.ErrorIs(t, err, http.ErrNotSupported)
}

// mockReaderFrom is a mock ResponseWriter that implements io.ReaderFrom, the
// interface net/http's own *response uses to hand a body to the connection so
// the kernel can serve it with sendfile(2).
type mockReaderFrom struct {
	http.ResponseWriter
	readFromCalled bool
}

func (m *mockReaderFrom) ReadFrom(src io.Reader) (int64, error) {
	m.readFromCalled = true
	return io.Copy(m.ResponseWriter, src)
}

// bodyReader returns the response body as an *io.LimitedReader, which is the
// shape http.ServeContent hands to io.Copy (via io.CopyN). It deliberately does
// not implement io.WriterTo, because io.Copy prefers a WriterTo source over a
// ReaderFrom destination and would then never consult the writer at all.
func bodyReader(body string) io.Reader {
	return io.LimitReader(strings.NewReader(body), int64(len(body)))
}

func TestWriterWrapper_ReadFromUsesUnderlyingFastPath(t *testing.T) {
	recorder := httptest.NewRecorder()
	mock := &mockReaderFrom{ResponseWriter: recorder}
	wrapper := &writerWrapper{
		ResponseWriter: mock,
		statusCode:     http.StatusOK,
	}

	// io.Copy is what http.ServeContent uses; it must reach the underlying
	// ReadFrom rather than falling back to a user-space copy loop.
	n, err := io.Copy(wrapper, bodyReader("payload"))

	require.NoError(t, err)
	assert.Equal(t, int64(len("payload")), n)
	assert.True(t, mock.readFromCalled, "io.Copy must reach the underlying io.ReaderFrom")
	assert.Equal(t, "payload", recorder.Body.String())
}

func TestWriterWrapper_ReadFrom_NotSupported(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &writerWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	// The recorder has no ReadFrom, so the wrapper copies through Write. This
	// must not recurse back into wrapper.ReadFrom.
	n, err := io.Copy(wrapper, bodyReader("payload"))

	require.NoError(t, err)
	assert.Equal(t, int64(len("payload")), n)
	assert.Equal(t, "payload", recorder.Body.String())
}

func TestWriterWrapper_ReadFrom_ImplicitStatusOK(t *testing.T) {
	recorder := httptest.NewRecorder()
	mock := &mockReaderFrom{ResponseWriter: recorder}
	wrapper := &writerWrapper{ResponseWriter: mock}

	_, err := wrapper.ReadFrom(bodyReader("payload"))

	require.NoError(t, err)
	assert.True(t, wrapper.wroteHeader)
	assert.Equal(t, http.StatusOK, wrapper.statusCode)
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestWriterWrapper_ReadFrom_KeepsExplicitStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	mock := &mockReaderFrom{ResponseWriter: recorder}
	wrapper := &writerWrapper{ResponseWriter: mock}

	wrapper.WriteHeader(http.StatusPartialContent)
	_, err := wrapper.ReadFrom(bodyReader("payload"))

	require.NoError(t, err)
	assert.Equal(t, http.StatusPartialContent, wrapper.statusCode)
	assert.Equal(t, http.StatusPartialContent, recorder.Code)
}

// mockStringWriter is a mock ResponseWriter that implements io.StringWriter,
// the interface net/http's own *response uses to write a string without an
// extra []byte conversion.
type mockStringWriter struct {
	http.ResponseWriter
	writeStringCalled bool
}

func (m *mockStringWriter) WriteString(s string) (int, error) {
	m.writeStringCalled = true
	return io.WriteString(m.ResponseWriter, s)
}

func TestWriterWrapper_WriteStringUsesUnderlyingFastPath(t *testing.T) {
	recorder := httptest.NewRecorder()
	mock := &mockStringWriter{ResponseWriter: recorder}
	wrapper := &writerWrapper{
		ResponseWriter: mock,
		statusCode:     http.StatusOK,
	}

	// io.WriteString is what fmt.Fprint and friends use; it must reach the
	// underlying WriteString rather than falling back to a []byte conversion.
	n, err := io.WriteString(wrapper, "payload")

	require.NoError(t, err)
	assert.Equal(t, len("payload"), n)
	assert.True(t, mock.writeStringCalled, "io.WriteString must reach the underlying io.StringWriter")
	assert.Equal(t, "payload", recorder.Body.String())
}

// plainResponseWriter implements exactly http.ResponseWriter and nothing more.
// httptest.ResponseRecorder cannot stand in for a writer that lacks a string
// fast path, because it does implement io.StringWriter, so a test built on the
// recorder would take WriteString's fast path and never reach the fallback.
type plainResponseWriter struct {
	rec         *httptest.ResponseRecorder
	writeCalled bool
}

func (p *plainResponseWriter) Header() http.Header { return p.rec.Header() }

func (p *plainResponseWriter) Write(b []byte) (int, error) {
	p.writeCalled = true
	return p.rec.Write(b)
}

func (p *plainResponseWriter) WriteHeader(statusCode int) { p.rec.WriteHeader(statusCode) }

func TestWriterWrapper_WriteString_NotSupported(t *testing.T) {
	recorder := httptest.NewRecorder()
	plain := &plainResponseWriter{rec: recorder}

	// Guard the premise of this test: if plainResponseWriter ever grows a
	// WriteString method, the wrapper would take the fast path and the
	// fallback below would silently stop being covered.
	var rw http.ResponseWriter = plain
	_, hasStringWriter := rw.(io.StringWriter)
	require.False(t, hasStringWriter, "plainResponseWriter must not implement io.StringWriter")

	wrapper := &writerWrapper{
		ResponseWriter: plain,
		statusCode:     http.StatusOK,
	}

	// plain has no WriteString, so the wrapper falls back to Write.
	n, err := io.WriteString(wrapper, "payload")

	require.NoError(t, err)
	assert.Equal(t, len("payload"), n)
	assert.True(t, plain.writeCalled, "the fallback must go through Write")
	assert.Equal(t, "payload", recorder.Body.String())
}

func TestWriterWrapper_WriteString_ImplicitStatusOK(t *testing.T) {
	recorder := httptest.NewRecorder()
	mock := &mockStringWriter{ResponseWriter: recorder}
	wrapper := &writerWrapper{ResponseWriter: mock}

	_, err := wrapper.WriteString("payload")

	require.NoError(t, err)
	assert.True(t, wrapper.wroteHeader)
	assert.Equal(t, http.StatusOK, wrapper.statusCode)
	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestWriterWrapper_WriteString_KeepsExplicitStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	mock := &mockStringWriter{ResponseWriter: recorder}
	wrapper := &writerWrapper{ResponseWriter: mock}

	wrapper.WriteHeader(http.StatusPartialContent)
	_, err := wrapper.WriteString("payload")

	require.NoError(t, err)
	assert.Equal(t, http.StatusPartialContent, wrapper.statusCode)
	assert.Equal(t, http.StatusPartialContent, recorder.Code)
}

func TestWriterWrapper_ServeFileOverRealServer(t *testing.T) {
	content := strings.Repeat("otelc", 100_000) // well past io.Copy's 32KiB buffer
	path := filepath.Join(t.TempDir(), "payload.txt")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	var underlyingIsReaderFrom bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The premise of writerWrapper.ReadFrom: net/http's own *response
		// implements io.ReaderFrom, which is what reaches sendfile(2).
		_, underlyingIsReaderFrom = w.(io.ReaderFrom)

		wrapper := &writerWrapper{ResponseWriter: w, statusCode: http.StatusOK}
		http.ServeFile(wrapper, r, path)
	}))
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.True(t, underlyingIsReaderFrom, "net/http ResponseWriter should implement io.ReaderFrom")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, content, string(body))
}

func TestWriterWrapper_Unwrap(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &writerWrapper{ResponseWriter: recorder}

	assert.Same(t, recorder, wrapper.Unwrap())
}

// mockDeadlineWriter is a mock ResponseWriter that supports read deadlines, a
// capability http.ResponseController can only reach through Unwrap.
type mockDeadlineWriter struct {
	http.ResponseWriter
	deadlineSet bool
}

func (m *mockDeadlineWriter) SetReadDeadline(time.Time) error {
	m.deadlineSet = true
	return nil
}

func TestWriterWrapper_ResponseControllerReachesUnderlyingWriter(t *testing.T) {
	mock := &mockDeadlineWriter{ResponseWriter: httptest.NewRecorder()}
	wrapper := &writerWrapper{ResponseWriter: mock}

	require.NoError(t, http.NewResponseController(wrapper).SetReadDeadline(time.Time{}))
	assert.True(t, mock.deadlineSet)
}

func TestWriterWrapper_MultipleStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"OK", http.StatusOK},
		{"Created", http.StatusCreated},
		{"BadRequest", http.StatusBadRequest},
		{"NotFound", http.StatusNotFound},
		{"InternalServerError", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			wrapper := &writerWrapper{
				ResponseWriter: recorder,
				statusCode:     http.StatusOK,
			}

			wrapper.WriteHeader(tt.statusCode)
			assert.Equal(t, tt.statusCode, wrapper.statusCode)
			assert.Equal(t, tt.statusCode, recorder.Code)
		})
	}
}
