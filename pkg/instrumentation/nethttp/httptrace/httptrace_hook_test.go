// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package httptrace

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http/httptrace"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setupTestTracer(t *testing.T) (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return sr, tp
}

func TestNewClientTrace_ReturnsValidTrace(t *testing.T) {
	_, tp := setupTestTracer(t)
	ctx := context.Background()

	ct := NewClientTrace(ctx, tp, "test")
	require.NotNil(t, ct)
	assert.NotNil(t, ct.GetConn)
	assert.NotNil(t, ct.GotConn)
	assert.NotNil(t, ct.DNSStart)
	assert.NotNil(t, ct.DNSDone)
	assert.NotNil(t, ct.ConnectStart)
	assert.NotNil(t, ct.ConnectDone)
	assert.NotNil(t, ct.TLSHandshakeStart)
	assert.NotNil(t, ct.TLSHandshakeDone)
	assert.NotNil(t, ct.WroteHeaders)
	assert.NotNil(t, ct.WroteRequest)
}

func TestNewClientTrace_NilTracerProvider(t *testing.T) {
	ctx := context.Background()

	// Should not panic with nil provider
	ct := NewClientTrace(ctx, nil, "test")
	require.NotNil(t, ct)

	// Calling hooks should not panic
	ct.GetConn("example.com:443")
}

func TestNewClientTrace_InheritsProviderFromSpan(t *testing.T) {
	sr, tp := setupTestTracer(t)
	parentCtx, parentSpan := tp.Tracer("test").Start(context.Background(), "parent")
	defer parentSpan.End()

	ct := NewClientTrace(parentCtx, nil, "test")
	require.NotNil(t, ct)

	ct.GetConn("example.com:443")
	ct.GotConn(httptrace.GotConnInfo{
		Conn: &mockConn{
			remoteAddr: &net.TCPAddr{IP: net.ParseIP("93.184.216.34"), Port: 443},
			localAddr:  &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 54321},
		},
	})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "http.getconn", spans[0].Name())
}

func TestDNSLifecycle(t *testing.T) {
	sr, tp := setupTestTracer(t)
	ctx := context.Background()

	ct := NewClientTrace(ctx, tp, "test")

	// Start getconn (parent of dns)
	ct.GetConn("example.com:443")

	// DNS lifecycle
	ct.DNSStart(httptrace.DNSStartInfo{Host: "example.com"})
	ct.DNSDone(httptrace.DNSDoneInfo{
		Addrs: []net.IPAddr{
			{IP: net.ParseIP("93.184.216.34")},
			{IP: net.ParseIP("2606:2800:220:1:248:1893:25c8:1946")},
		},
	})

	// End getconn
	ct.GotConn(httptrace.GotConnInfo{
		Conn: &mockConn{
			remoteAddr: &net.TCPAddr{IP: net.ParseIP("93.184.216.34"), Port: 443},
			localAddr:  &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 54321},
		},
	})

	spans := sr.Ended()
	require.Len(t, spans, 2)

	// DNS should end first (child)
	dnsSpan := spans[0]
	assert.Equal(t, "http.dns", dnsSpan.Name())

	// Verify DNS start attributes
	hasHostAttr := false
	for _, attr := range dnsSpan.Attributes() {
		if attr.Key == HTTPHostAttribute {
			hasHostAttr = true
			assert.Equal(t, "example.com", attr.Value.AsString())
		}
	}
	assert.True(t, hasHostAttr, "dns span should have net.host.name attribute")

	// GetConn should end second (parent)
	getconnSpan := spans[1]
	assert.Equal(t, "http.getconn", getconnSpan.Name())
}

func TestDNSFailure(t *testing.T) {
	sr, tp := setupTestTracer(t)
	ctx := context.Background()

	ct := NewClientTrace(ctx, tp, "test")
	ct.GetConn("nonexistent.example.com:443")

	ct.DNSStart(httptrace.DNSStartInfo{Host: "nonexistent.example.com"})
	dnsErr := errors.New("no such host")
	ct.DNSDone(httptrace.DNSDoneInfo{Err: dnsErr})

	ct.GotConn(httptrace.GotConnInfo{
		Conn: &mockConn{
			remoteAddr: &net.TCPAddr{},
			localAddr:  &net.TCPAddr{},
		},
	})

	spans := sr.Ended()
	require.Len(t, spans, 2)

	dnsSpan := spans[0]
	assert.Equal(t, "http.dns", dnsSpan.Name())
	assert.Equal(t, "no such host", dnsSpan.Status().Description)
}

func TestConnectLifecycle(t *testing.T) {
	sr, tp := setupTestTracer(t)
	ctx := context.Background()

	ct := NewClientTrace(ctx, tp, "test")
	ct.GetConn("example.com:443")

	ct.ConnectStart("tcp", "93.184.216.34:443")
	ct.ConnectDone("tcp", "93.184.216.34:443", nil)

	ct.GotConn(httptrace.GotConnInfo{
		Conn: &mockConn{
			remoteAddr: &net.TCPAddr{IP: net.ParseIP("93.184.216.34"), Port: 443},
			localAddr:  &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 54321},
		},
	})

	spans := sr.Ended()
	require.Len(t, spans, 2)

	connectSpan := spans[0]
	assert.Equal(t, "http.connect", connectSpan.Name())

	hasRemoteAddr := false
	for _, attr := range connectSpan.Attributes() {
		if attr.Key == HTTPRemoteAddr {
			hasRemoteAddr = true
		}
	}
	assert.True(t, hasRemoteAddr, "connect span should have http.remote attribute")
}

func TestConnectFailure(t *testing.T) {
	sr, tp := setupTestTracer(t)
	ctx := context.Background()

	ct := NewClientTrace(ctx, tp, "test")
	ct.GetConn("example.com:443")

	ct.ConnectStart("tcp", "93.184.216.34:443")
	connErr := errors.New("connection refused")
	ct.ConnectDone("tcp", "93.184.216.34:443", connErr)

	ct.GotConn(httptrace.GotConnInfo{
		Conn: &mockConn{
			remoteAddr: &net.TCPAddr{},
			localAddr:  &net.TCPAddr{},
		},
	})

	spans := sr.Ended()
	require.Len(t, spans, 2)

	connectSpan := spans[0]
	assert.Equal(t, "http.connect", connectSpan.Name())
	assert.Equal(t, "connection refused", connectSpan.Status().Description)
}

func TestTLSLifecycle(t *testing.T) {
	sr, tp := setupTestTracer(t)
	ctx := context.Background()

	ct := NewClientTrace(ctx, tp, "test")
	ct.GetConn("example.com:443")

	ct.TLSHandshakeStart()
	ct.TLSHandshakeDone(tls.ConnectionState{}, nil)

	ct.GotConn(httptrace.GotConnInfo{
		Conn: &mockConn{
			remoteAddr: &net.TCPAddr{IP: net.ParseIP("93.184.216.34"), Port: 443},
			localAddr:  &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 54321},
		},
	})

	spans := sr.Ended()
	require.Len(t, spans, 2)

	tlsSpan := spans[0]
	assert.Equal(t, "http.tls", tlsSpan.Name())
}

func TestTLSFailure(t *testing.T) {
	sr, tp := setupTestTracer(t)
	ctx := context.Background()

	ct := NewClientTrace(ctx, tp, "test")
	ct.GetConn("example.com:443")

	ct.TLSHandshakeStart()
	tlsErr := errors.New("certificate verify failed")
	ct.TLSHandshakeDone(tls.ConnectionState{}, tlsErr)

	ct.GotConn(httptrace.GotConnInfo{
		Conn: &mockConn{
			remoteAddr: &net.TCPAddr{},
			localAddr:  &net.TCPAddr{},
		},
	})

	spans := sr.Ended()
	require.Len(t, spans, 2)

	tlsSpan := spans[0]
	assert.Equal(t, "http.tls", tlsSpan.Name())
	assert.Equal(t, "certificate verify failed", tlsSpan.Status().Description)
}



func TestWroteRequestError(t *testing.T) {
	sr, tp := setupTestTracer(t)
	ctx := context.Background()

	ct := NewClientTrace(ctx, tp, "test")

	ct.WroteHeaders()
	writeErr := errors.New("broken pipe")
	ct.WroteRequest(httptrace.WroteRequestInfo{Err: writeErr})

	spans := sr.Ended()
	require.Len(t, spans, 1)

	sendSpan := spans[0]
	assert.Equal(t, "http.send", sendSpan.Name())
	assert.Equal(t, "broken pipe", sendSpan.Status().Description)
}

func TestConnectionReusedAttributes(t *testing.T) {
	sr, tp := setupTestTracer(t)
	ctx := context.Background()

	ct := NewClientTrace(ctx, tp, "test")
	ct.GetConn("example.com:443")

	ct.GotConn(httptrace.GotConnInfo{
		Conn: &mockConn{
			remoteAddr: &net.TCPAddr{IP: net.ParseIP("93.184.216.34"), Port: 443},
			localAddr:  &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 54321},
		},
		Reused:   true,
		WasIdle:  true,
		IdleTime: 5 * time.Second,
	})

	spans := sr.Ended()
	require.Len(t, spans, 1)

	getconnSpan := spans[0]
	assert.Equal(t, "http.getconn", getconnSpan.Name())

	var foundReused, foundWasIdle, foundIdleTime bool
	for _, attr := range getconnSpan.Attributes() {
		switch attr.Key {
		case HTTPConnectionReused:
			foundReused = true
			assert.True(t, attr.Value.AsBool())
		case HTTPConnectionWasIdle:
			foundWasIdle = true
			assert.True(t, attr.Value.AsBool())
		case HTTPConnectionIdleTime:
			foundIdleTime = true
			assert.Equal(t, "5s", attr.Value.AsString())
		}
	}
	assert.True(t, foundReused, "should have http.conn.reused attribute")
	assert.True(t, foundWasIdle, "should have http.conn.wasidle attribute")
	assert.True(t, foundIdleTime, "should have http.conn.idletime attribute")
}

func TestFullRequestLifecycle(t *testing.T) {
	sr, tp := setupTestTracer(t)

	parentCtx, parentSpan := tp.Tracer("test").Start(context.Background(), "HTTP GET")
	defer parentSpan.End()

	ct := NewClientTrace(parentCtx, tp, "test")

	// 1. GetConn
	ct.GetConn("example.com:443")

	// 2. DNS
	ct.DNSStart(httptrace.DNSStartInfo{Host: "example.com"})
	ct.DNSDone(httptrace.DNSDoneInfo{
		Addrs: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}},
	})

	// 3. Connect
	ct.ConnectStart("tcp", "93.184.216.34:443")
	ct.ConnectDone("tcp", "93.184.216.34:443", nil)

	// 4. TLS
	ct.TLSHandshakeStart()
	ct.TLSHandshakeDone(tls.ConnectionState{}, nil)

	// 5. GotConn
	ct.GotConn(httptrace.GotConnInfo{
		Conn: &mockConn{
			remoteAddr: &net.TCPAddr{IP: net.ParseIP("93.184.216.34"), Port: 443},
			localAddr:  &net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 54321},
		},
	})

	// 6. Send
	ct.WroteHeaders()
	ct.WroteRequest(httptrace.WroteRequestInfo{})

	spans := sr.Ended()
	// dns, connect, tls, getconn, send = 5 sub-spans
	require.Len(t, spans, 5)

	spanNames := make([]string, len(spans))
	for i, s := range spans {
		spanNames[i] = s.Name()
	}

	assert.Contains(t, spanNames, "http.dns")
	assert.Contains(t, spanNames, "http.connect")
	assert.Contains(t, spanNames, "http.tls")
	assert.Contains(t, spanNames, "http.getconn")
	assert.Contains(t, spanNames, "http.send")
}

func TestParentHook(t *testing.T) {
	tests := []struct {
		hook     string
		expected string
	}{
		{"http.dns", "http.getconn"},
		{"http.connect.93.184.216.34:443", "http.getconn"},
		{"http.tls", "http.getconn"},
		{"http.send", ""},
	}

	for _, tt := range tests {
		t.Run(tt.hook, func(t *testing.T) {
			result := parentHook(tt.hook)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// mockConn implements net.Conn for testing GotConnInfo.
type mockConn struct {
	remoteAddr net.Addr
	localAddr  net.Addr
}

func (c *mockConn) Read(_ []byte) (int, error)         { return 0, nil }
func (c *mockConn) Write(_ []byte) (int, error)        { return 0, nil }
func (c *mockConn) Close() error                       { return nil }
func (c *mockConn) LocalAddr() net.Addr                { return c.localAddr }
func (c *mockConn) RemoteAddr() net.Addr               { return c.remoteAddr }
func (c *mockConn) SetDeadline(_ time.Time) error      { return nil }
func (c *mockConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *mockConn) SetWriteDeadline(_ time.Time) error { return nil }
