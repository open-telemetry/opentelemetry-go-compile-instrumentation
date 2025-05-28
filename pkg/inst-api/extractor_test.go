// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrumenter

import (
	"errors"
	"testing"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type testSpan struct {
	trace.Span
	status *codes.Code
}

func (ts testSpan) SetStatus(status codes.Code, _ string) {
	*ts.status = status
}

func TestDefaultSpanStatusExtractor(t *testing.T) {
	unset := codes.Unset
	ts := testSpan{Span: noop.Span{}, status: &unset}
	d := defaultSpanStatusExtractor[any, any]{}
	d.Extract(ts, nil, nil, errors.New(""))
	if *ts.status != codes.Error {
		t.Fatal("expected error code")
	}
}

func TestAlwaysInternalExtractor(t *testing.T) {
	a := &AlwaysInternalExtractor[any]{}
	kind := a.Extract(nil)
	if kind != trace.SpanKindInternal {
		t.Fatal("expected internal kind")
	}
}

func TestAlwaysServerExtractor(t *testing.T) {
	a := &AlwaysServerExtractor[any]{}
	kind := a.Extract(nil)
	if kind != trace.SpanKindServer {
		t.Fatal("expected server kind")
	}
}

func TestAlwaysClientExtractor(t *testing.T) {
	a := &AlwaysClientExtractor[any]{}
	kind := a.Extract(nil)
	if kind != trace.SpanKindClient {
		t.Fatal("expected client kind")
	}
}

func TestAlwaysConsumerExtractor(t *testing.T) {
	a := &AlwaysConsumerExtractor[any]{}
	kind := a.Extract(nil)
	if kind != trace.SpanKindConsumer {
		t.Fatal("expected consumer kind")
	}
}

func TestAlwaysProducerExtractor(t *testing.T) {
	a := &AlwaysProducerExtractor[any]{}
	kind := a.Extract(nil)
	if kind != trace.SpanKindProducer {
		t.Fatal("expected producer kind")
	}
}
