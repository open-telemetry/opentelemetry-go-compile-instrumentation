// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package httptrace

import (
	"context"
	"crypto/tls"
	"net/http/httptrace"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const scopeName = "go.opentelemetry.io/compile-instrumentation/httptrace"

// Attribute keys for HTTP connection lifecycle events.
var (
	HTTPRemoteAddr             = attribute.Key("http.remote")
	HTTPLocalAddr              = attribute.Key("http.local")
	HTTPConnectionReused       = attribute.Key("http.conn.reused")
	HTTPConnectionWasIdle      = attribute.Key("http.conn.wasidle")
	HTTPConnectionIdleTime     = attribute.Key("http.conn.idletime")
	HTTPConnectionStartNetwork = attribute.Key("http.conn.start.network")
	HTTPConnectionDoneNetwork  = attribute.Key("http.conn.done.network")
	HTTPConnectionDoneAddr     = attribute.Key("http.conn.done.addr")
	HTTPDNSAddrs               = attribute.Key("http.dns.addrs")
	HTTPHostAttribute          = attribute.Key("net.host.name")
)

// hookParentMap maps child hook names to their logical parent span.
var hookParentMap = map[string]string{
	"http.dns":     "http.getconn",
	"http.connect": "http.getconn",
	"http.tls":     "http.getconn",
}

func parentHook(hook string) string {
	if strings.HasPrefix(hook, "http.connect") {
		return hookParentMap["http.connect"]
	}
	return hookParentMap[hook]
}

// clientTracer tracks active sub-spans for each HTTP request lifecycle phase.
type clientTracer struct {
	context.Context

	tr          trace.Tracer
	activeHooks map[string]context.Context
	root        trace.Span
	mtx         sync.Mutex
}

// NewClientTrace returns an httptrace.ClientTrace that records OpenTelemetry
// sub-spans for DNS resolution, TCP connection, TLS handshake, header writing,
// request send, and response receive phases.
func NewClientTrace(ctx context.Context, tp trace.TracerProvider, version string) *httptrace.ClientTrace {
	ct := &clientTracer{
		Context:     ctx,
		activeHooks: make(map[string]context.Context),
	}

	if tp == nil {
		if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
			tp = span.TracerProvider()
		}
	}

	if tp != nil {
		ct.tr = tp.Tracer(
			scopeName,
			trace.WithInstrumentationVersion(version),
		)
	}

	return &httptrace.ClientTrace{
		GetConn:              ct.getConn,
		GotConn:              ct.gotConn,
		PutIdleConn:          ct.putIdleConn,
		GotFirstResponseByte: ct.gotFirstResponseByte,
		DNSStart:             ct.dnsStart,
		DNSDone:              ct.dnsDone,
		ConnectStart:         ct.connectStart,
		ConnectDone:          ct.connectDone,
		TLSHandshakeStart:    ct.tlsHandshakeStart,
		TLSHandshakeDone:     ct.tlsHandshakeDone,
		WroteHeaders:         ct.wroteHeaders,
		WroteRequest:         ct.wroteRequest,
	}
}

func (ct *clientTracer) start(hook, spanName string, attrs ...attribute.KeyValue) {
	if ct.tr == nil {
		return
	}

	ct.mtx.Lock()
	defer ct.mtx.Unlock()

	if hookCtx, found := ct.activeHooks[hook]; !found {
		var sp trace.Span
		ct.activeHooks[hook], sp = ct.tr.Start(
			ct.getParentContext(hook),
			spanName,
			trace.WithAttributes(attrs...),
			trace.WithSpanKind(trace.SpanKindClient),
		)
		if ct.root == nil {
			ct.root = sp
		}
	} else {
		// end was called before start finished — attach start attrs, end the span
		span := trace.SpanFromContext(hookCtx)
		span.SetAttributes(attrs...)
		span.End()
		delete(ct.activeHooks, hook)
	}
}

func (ct *clientTracer) end(hook string, err error, attrs ...attribute.KeyValue) {
	if ct.tr == nil {
		return
	}

	ct.mtx.Lock()
	defer ct.mtx.Unlock()

	if ctx, ok := ct.activeHooks[hook]; ok {
		span := trace.SpanFromContext(ctx)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.SetAttributes(attrs...)
		span.End()
		delete(ct.activeHooks, hook)
	} else {
		// start hasn't finished yet — create a span with end attrs
		ctx, span := ct.tr.Start(
			ct.getParentContext(hook),
			hook,
			trace.WithAttributes(attrs...),
			trace.WithSpanKind(trace.SpanKindClient),
		)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		ct.activeHooks[hook] = ctx
	}
}

func (ct *clientTracer) getParentContext(hook string) context.Context {
	ctx, ok := ct.activeHooks[parentHook(hook)]
	if !ok {
		return ct.Context
	}
	return ctx
}

func (ct *clientTracer) getConn(host string) {
	ct.start("http.getconn", "http.getconn", HTTPHostAttribute.String(host))
}

func (ct *clientTracer) gotConn(info httptrace.GotConnInfo) {
	attrs := []attribute.KeyValue{
		HTTPRemoteAddr.String(info.Conn.RemoteAddr().String()),
		HTTPLocalAddr.String(info.Conn.LocalAddr().String()),
		HTTPConnectionReused.Bool(info.Reused),
		HTTPConnectionWasIdle.Bool(info.WasIdle),
	}
	if info.WasIdle {
		attrs = append(attrs, HTTPConnectionIdleTime.String(info.IdleTime.String()))
	}
	ct.end("http.getconn", nil, attrs...)
}

func (ct *clientTracer) putIdleConn(err error) {
	ct.end("http.receive", err)
}

func (ct *clientTracer) gotFirstResponseByte() {
	ct.start("http.receive", "http.receive")
}

func (ct *clientTracer) dnsStart(info httptrace.DNSStartInfo) {
	ct.start("http.dns", "http.dns", HTTPHostAttribute.String(info.Host))
}

func (ct *clientTracer) dnsDone(info httptrace.DNSDoneInfo) {
	addrs := make([]string, 0, len(info.Addrs))
	for _, netAddr := range info.Addrs {
		addrs = append(addrs, netAddr.String())
	}
	ct.end("http.dns", info.Err, HTTPDNSAddrs.String(strings.Join(addrs, ",")))
}

func (ct *clientTracer) connectStart(network, addr string) {
	ct.start("http.connect."+addr, "http.connect",
		HTTPRemoteAddr.String(addr),
		HTTPConnectionStartNetwork.String(network),
	)
}

func (ct *clientTracer) connectDone(network, addr string, err error) {
	ct.end("http.connect."+addr, err,
		HTTPConnectionDoneAddr.String(addr),
		HTTPConnectionDoneNetwork.String(network),
	)
}

func (ct *clientTracer) tlsHandshakeStart() {
	ct.start("http.tls", "http.tls")
}

func (ct *clientTracer) tlsHandshakeDone(_ tls.ConnectionState, err error) {
	ct.end("http.tls", err)
}

func (ct *clientTracer) wroteHeaders() {
	ct.start("http.send", "http.send")
}

func (ct *clientTracer) wroteRequest(info httptrace.WroteRequestInfo) {
	if info.Err != nil && ct.root != nil {
		ct.root.SetStatus(codes.Error, info.Err.Error())
	}
	ct.end("http.send", info.Err)
}
