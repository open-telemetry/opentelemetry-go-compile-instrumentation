// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestWriterWrapper_Pusher(t *testing.T) {
	mock := &mockPusher{ResponseWriter: httptest.NewRecorder()}
	wrapper := &writerWrapper{
		ResponseWriter: mock,
		statusCode:     http.StatusOK,
	}

	pusher := wrapper.Pusher()
	require.NotNil(t, pusher)

	err := pusher.Push("/test", nil)
	require.NoError(t, err)
	assert.True(t, mock.pushCalled)
}

func TestWriterWrapper_Pusher_NotSupported(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &writerWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	pusher := wrapper.Pusher()
	assert.Nil(t, pusher)
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
