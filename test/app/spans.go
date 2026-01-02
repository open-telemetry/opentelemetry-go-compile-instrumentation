// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// AllSpans returns all spans from trace data as a flat list.
func AllSpans(td ptrace.Traces) []ptrace.Span {
	spans := make([]ptrace.Span, 0)
	for i := range td.ResourceSpans().Len() {
		for j := range td.ResourceSpans().At(i).ScopeSpans().Len() {
			ss := td.ResourceSpans().At(i).ScopeSpans().At(j).Spans()
			for k := range ss.Len() {
				spans = append(spans, ss.At(k))
			}
		}
	}
	return spans
}

// Attrs converts span attributes to a map.
func Attrs(s ptrace.Span) map[string]any {
	m := make(map[string]any)
	s.Attributes().Range(func(k string, v pcommon.Value) bool {
		m[k] = v.AsRaw()
		return true
	})
	return m
}

// SpanMatcher is a predicate for filtering spans.
type SpanMatcher func(ptrace.Span) bool

// IsClient matches client spans.
func IsClient(s ptrace.Span) bool { return s.Kind() == ptrace.SpanKindClient }

// IsServer matches server spans.
func IsServer(s ptrace.Span) bool { return s.Kind() == ptrace.SpanKindServer }

// HasAttribute matches spans with an exact attribute value.
func HasAttribute(key string, value any) SpanMatcher {
	return func(s ptrace.Span) bool {
		v, ok := Attrs(s)[key]
		return ok && v == value
	}
}

// HasAttributeContaining matches spans where a string attribute contains a substring.
func HasAttributeContaining(key, substr string) SpanMatcher {
	return func(s ptrace.Span) bool {
		v, ok := Attrs(s)[key].(string)
		return ok && strings.Contains(v, substr)
	}
}

// RequireSpan finds a span matching all matchers or fails.
func RequireSpan(t *testing.T, td ptrace.Traces, matchers ...SpanMatcher) ptrace.Span {
	for _, s := range AllSpans(td) {
		match := true
		for _, m := range matchers {
			if !m(s) {
				match = false
				break
			}
		}
		if match {
			return s
		}
	}
	require.Fail(t, "No span found matching criteria")
	return ptrace.NewSpan()
}

// RequireAttribute asserts a span has an attribute with expected value.
func RequireAttribute(t *testing.T, s ptrace.Span, key string, expected any) {
	v, ok := Attrs(s)[key]
	require.True(t, ok, "Attribute %q not found in span %q", key, s.Name())
	require.Equal(t, expected, v, "Attribute %q mismatch in span %q", key, s.Name())
}

// RequireAttributeExists asserts a span has an attribute.
func RequireAttributeExists(t *testing.T, s ptrace.Span, key string) {
	_, ok := Attrs(s)[key]
	require.True(t, ok, "Attribute %q not found in span %q", key, s.Name())
}

// TraceStats holds trace statistics.
type TraceStats struct {
	TraceCount    int
	TotalSpans    int
	SpansPerTrace map[string]int
}

// AnalyzeTraces collects trace statistics.
func AnalyzeTraces(t *testing.T, td ptrace.Traces) TraceStats {
	stats := TraceStats{SpansPerTrace: make(map[string]int)}
	for _, s := range AllSpans(td) {
		t.Logf("Span: name=%s, kind=%v, attrs=%v", s.Name(), s.Kind(), Attrs(s))
		tid := s.TraceID()
		stats.SpansPerTrace[hex.EncodeToString(tid[:])]++
		stats.TotalSpans++
	}
	stats.TraceCount = len(stats.SpansPerTrace)
	return stats
}

// String returns a human-readable representation of trace statistics.
func (ts TraceStats) String() string {
	if ts.TraceCount == 0 {
		return "No traces found"
	}
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "Found %d trace(s) with %d span(s):\n", ts.TraceCount, ts.TotalSpans)
	for id, n := range ts.SpansPerTrace {
		_, _ = fmt.Fprintf(&b, "  - Trace %s...: %d span(s)\n", id[:16], n)
	}
	return b.String()
}
