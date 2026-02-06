// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// TestFixture provides common setup for e2e and integration tests.
type TestFixture struct {
	t         *testing.T
	collector *Collector
	appsDir   string // Directory for self-contained test apps (test/apps/)

	serviceName   string
	skipCollector bool
}

type TestFixtureOption func(*TestFixture)

func WithServiceName(name string) TestFixtureOption {
	return func(f *TestFixture) {
		f.serviceName = name
	}
}

func WithoutCollector() TestFixtureOption {
	return func(f *TestFixture) {
		f.skipCollector = true
	}
}

// NewTestFixture creates a new test fixture.
// It automatically starts the collector and sets up OTEL env vars.
// Tests can override env vars after calling this if needed.
func NewTestFixture(t *testing.T, opts ...TestFixtureOption) *TestFixture {
	f := &TestFixture{
		t:           t,
		serviceName: "test-service",
	}

	for _, opt := range opts {
		opt(f)
	}

	pwd, err := os.Getwd()
	require.NoError(t, err)
	f.appsDir = filepath.Join(pwd, "..", "apps")

	// Start collector unless skipped
	if !f.skipCollector {
		f.collector = StartCollector(t)

		// Configure OTEL env vars (can be overridden by test after this)
		// Clear signal-specific endpoints to ensure OTEL_EXPORTER_OTLP_ENDPOINT takes precedence
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
		t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
		t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")
		t.Setenv("OTEL_SERVICE_NAME", f.serviceName)
		t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", f.collector.URL)
		t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
		t.Setenv("OTEL_GO_SIMPLE_SPAN_PROCESSOR", "true")
	}

	return f
}

// Traces returns the collected traces for assertions.
func (f *TestFixture) Traces() ptrace.Traces {
	return f.collector.Traces
}

// CollectorURL returns the collector endpoint URL.
func (f *TestFixture) CollectorURL() string {
	return f.collector.URL
}

// resolveAppPath converts an app name like "httpserver" to full path in test/apps/.
func (f *TestFixture) resolveAppPath(appName string) string {
	return filepath.Join(f.appsDir, appName)
}

// Build builds a test application from test/apps/ with the instrumentation tool.
func (f *TestFixture) Build(appName string) {
	Build(f.t, f.resolveAppPath(appName), "go", "build", "-a")
}

// Server represents a running server process.
type Server struct {
	t       *testing.T
	appPath string
}

// Start starts a test application from test/apps/ and waits for it to be ready.
func (f *TestFixture) Start(appName string, args ...string) *Server {
	fullPath := f.resolveAppPath(appName)
	Start(f.t, fullPath, args...)

	return &Server{
		t:       f.t,
		appPath: appName,
	}
}

// Run runs a test application from test/apps/ and waits for it to complete.
func (f *TestFixture) Run(appName string, args ...string) string {
	return Run(f.t, f.resolveAppPath(appName), args...)
}

// BuildAndStart builds and starts a test application
func (f *TestFixture) BuildAndStart(appName string, args ...string) *Server {
	f.Build(appName)
	return f.Start(appName, args...)
}

// BuildAndRun builds and runs a test application
func (f *TestFixture) BuildAndRun(appName string, args ...string) string {
	f.Build(appName)
	return f.Run(appName, args...)
}

// RequireTraceCount asserts the expected number of traces were collected.
func (f *TestFixture) RequireTraceCount(expected int) {
	stats := AnalyzeTraces(f.t, f.collector.Traces)
	require.Equal(f.t, expected, stats.TraceCount,
		"Expected %d traces, got %d. %s", expected, stats.TraceCount, stats.String())
}

// RequireSpansPerTrace asserts each trace has the expected number of spans.
func (f *TestFixture) RequireSpansPerTrace(expected int) {
	stats := AnalyzeTraces(f.t, f.collector.Traces)
	for traceID, count := range stats.SpansPerTrace {
		require.Equal(f.t, expected, count,
			"Trace %s should have %d spans, got %d", traceID[:16], expected, count)
	}
}

// RequireSingleSpan asserts exactly 1 trace with 1 span and returns it.
func (f *TestFixture) RequireSingleSpan() ptrace.Span {
	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(1)
	spans := AllSpans(f.collector.Traces)
	require.Len(f.t, spans, 1, "Expected exactly 1 span")
	return spans[0]
}
