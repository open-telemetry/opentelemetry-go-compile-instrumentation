// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
	require.ErrorIs(t, err, http.ErrNotSupported)
	assert.Nil(t, conn)
	assert.Nil(t, rw)
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

// readerFromRecorder records whether the sendfile fast path was taken.
type readerFromRecorder struct {
	http.ResponseWriter
	readFromCalled bool
}

func (r *readerFromRecorder) ReadFrom(src io.Reader) (int64, error) {
	r.readFromCalled = true
	return io.Copy(r.ResponseWriter, src)
}

func TestWriterWrapper_ReadFrom_PreservesFastPath(t *testing.T) {
	recorder := httptest.NewRecorder()
	mock := &readerFromRecorder{ResponseWriter: recorder}
	wrapper := &writerWrapper{
		ResponseWriter: mock,
		statusCode:     http.StatusOK,
	}

	// Wrap in a bare io.Reader: strings.Reader implements io.WriterTo, which
	// io.Copy prefers, and that would bypass ReadFrom entirely. Real bodies
	// (e.g. the *os.File behind http.ServeFile) take the ReadFrom path.
	src := struct{ io.Reader }{strings.NewReader("payload")}
	n, err := io.Copy(wrapper, src)

	require.NoError(t, err)
	assert.Equal(t, int64(len("payload")), n)
	assert.True(t, mock.readFromCalled, "io.Copy must reach the underlying io.ReaderFrom")
	assert.Equal(t, "payload", recorder.Body.String())
	// The implicit 200 must still be captured on the ReadFrom path.
	assert.True(t, wrapper.wroteHeader)
	assert.Equal(t, http.StatusOK, wrapper.statusCode)
}

func TestWriterWrapper_ReadFrom_Fallback(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &writerWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	n, err := wrapper.ReadFrom(strings.NewReader("payload"))

	require.NoError(t, err)
	assert.Equal(t, int64(len("payload")), n)
	assert.Equal(t, "payload", recorder.Body.String())
	assert.True(t, wrapper.wroteHeader)
	assert.Equal(t, http.StatusOK, wrapper.statusCode)
}

type errorReader struct {
	err error
}

func (e *errorReader) Read([]byte) (int, error) {
	return 0, e.err
}

func TestWriterWrapper_ReadFrom_Error_DefersHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	mock := &readerFromRecorder{ResponseWriter: recorder}
	wrapper := &writerWrapper{
		ResponseWriter: mock,
		statusCode:     http.StatusOK,
	}

	testErr := io.ErrUnexpectedEOF
	src := struct{ io.Reader }{&errorReader{err: testErr}}
	n, err := io.Copy(wrapper, src)

	require.ErrorIs(t, err, testErr)
	assert.Equal(t, int64(0), n)
	assert.True(t, mock.readFromCalled)
	// Header must NOT be committed yet when no bytes were written.
	assert.False(t, wrapper.wroteHeader)

	// Handler should be able to report an error status.
	http.Error(wrapper, "error occurred", http.StatusInternalServerError)
	assert.True(t, wrapper.wroteHeader)
	assert.Equal(t, http.StatusInternalServerError, wrapper.statusCode)
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func TestWriterWrapper_ReadFrom_Fallback_Error_DefersHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &writerWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	testErr := io.ErrUnexpectedEOF
	n, err := wrapper.ReadFrom(&errorReader{err: testErr})

	require.ErrorIs(t, err, testErr)
	assert.Equal(t, int64(0), n)
	assert.False(t, wrapper.wroteHeader)

	http.Error(wrapper, "error occurred", http.StatusInternalServerError)
	assert.True(t, wrapper.wroteHeader)
	assert.Equal(t, http.StatusInternalServerError, wrapper.statusCode)
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func TestWriterWrapper_ReadFrom_PreWrittenHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	mock := &readerFromRecorder{ResponseWriter: recorder}
	wrapper := &writerWrapper{
		ResponseWriter: mock,
		statusCode:     http.StatusOK,
	}

	wrapper.WriteHeader(http.StatusAccepted)
	assert.True(t, wrapper.wroteHeader)
	assert.Equal(t, http.StatusAccepted, wrapper.statusCode)

	src := struct{ io.Reader }{strings.NewReader("data")}
	n, err := io.Copy(wrapper, src)

	require.NoError(t, err)
	assert.Equal(t, int64(4), n)
	assert.Equal(t, http.StatusAccepted, wrapper.statusCode)
	assert.Equal(t, http.StatusAccepted, recorder.Code)
}

func TestWriterWrapper_FlushError_NotSupported(t *testing.T) {
	wrapper := &writerWrapper{
		// A bare ResponseWriter with no Flush support.
		ResponseWriter: nonFlusher{httptest.NewRecorder()},
		statusCode:     http.StatusOK,
	}

	require.ErrorIs(t, wrapper.FlushError(), http.ErrNotSupported)
}

// nonFlusher hides the recorder's Flush method.
type nonFlusher struct{ rec *httptest.ResponseRecorder }

func (n nonFlusher) Header() http.Header         { return n.rec.Header() }
func (n nonFlusher) Write(b []byte) (int, error) { return n.rec.Write(b) }
func (n nonFlusher) WriteHeader(code int)        { n.rec.WriteHeader(code) }
