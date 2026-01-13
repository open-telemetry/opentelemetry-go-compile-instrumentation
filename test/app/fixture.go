// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// E2EFixture provides common setup for e2e and integration tests.
type E2EFixture struct {
	t         *testing.T
	collector *Collector
	demoDir   string
	appsDir   string // Directory for self-contained test apps (test/apps/)

	ServiceName   string
	skipCollector bool
}

type E2EFixtureOption func(*E2EFixture)

func WithServiceName(name string) E2EFixtureOption {
	return func(f *E2EFixture) {
		f.ServiceName = name
	}
}

func WithoutCollector() E2EFixtureOption {
	return func(f *E2EFixture) {
		f.skipCollector = true
	}
}

// NewE2EFixture creates a new e2e test fixture.
// It automatically starts the collector and sets up OTEL env vars.
// Tests can override env vars after calling this if needed.
func NewE2EFixture(t *testing.T, opts ...E2EFixtureOption) *E2EFixture {
	f := &E2EFixture{
		t:           t,
		ServiceName: "test-service",
	}

	for _, opt := range opts {
		opt(f)
	}

	pwd, err := os.Getwd()
	require.NoError(t, err)
	f.demoDir = filepath.Join(pwd, "..", "..", "demo")
	f.appsDir = filepath.Join(pwd, "..", "apps")

	// Start collector unless skipped
	if !f.skipCollector {
		f.collector = StartCollector(t)

		// Configure OTEL env vars (can be overridden by test after this)
		t.Setenv("OTEL_SERVICE_NAME", f.ServiceName)
		t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", f.collector.URL)
		t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	}

	return f
}

// Traces returns the collected traces for assertions.
func (f *E2EFixture) Traces() ptrace.Traces {
	return f.collector.Traces
}

// CollectorURL returns the collector endpoint URL.
func (f *E2EFixture) CollectorURL() string {
	return f.collector.URL
}

// // resolvePath converts a relative app path like "http/server" to full path.
// // Deprecated: Use resolveAppPath for test/apps.
// func (f *E2EFixture) resolvePath(appPath string) string {
// 	return filepath.Join(f.demoDir, appPath)
// }

// resolveAppPath converts an app name like "httpserver" to full path in test/apps/.
func (f *E2EFixture) resolveAppPath(appName string) string {
	return filepath.Join(f.appsDir, appName)
}

// // Build builds an application with the instrumentation tool.
// // appPath is relative to the demo directory, e.g., "http/server".
// // Deprecated: Use BuildApp for test/apps.
// func (f *E2EFixture) Build(appPath string) {
// 	Build(f.t, f.resolvePath(appPath), "go", "build", "-a")
// }

// BuildApp builds a test application from test/apps/ with the instrumentation tool.
// appName is the directory name in test/apps/, e.g., "httpserver".
func (f *E2EFixture) BuildApp(appName string) {
	Build(f.t, f.resolveAppPath(appName), "go", "build", "-a")
}

// // BuildPlain builds an application WITHOUT instrumentation (regular go build).
// // Useful for testing client/server in isolation.
// func (f *E2EFixture) BuildPlain(appPath string) {
// 	BuildPlain(f.t, f.resolvePath(appPath))
// }

// Server represents a running server process.
type Server struct {
	t       *testing.T
	appPath string
}

// // StartServer starts a server application and waits for it to be ready.
// // appPath is relative to the demo directory, e.g., "http/server".
// // It returns a Server that can be stopped to get the output.
// // Deprecated: Use StartApp for test/apps.
// func (f *E2EFixture) StartServer(appPath string, args ...string) *Server {
// 	fullPath := f.resolvePath(appPath)
// 	cmd, output := Start(f.t, fullPath, args...)
// 	stopFn, err := WaitForServerReady(f.t, cmd, output)
// 	require.NoError(f.t, err)

// 	return &Server{
// 		t:       f.t,
// 		stopFn:  stopFn,
// 		appPath: appPath,
// 	}
// }

// StartApp starts a test application from test/apps/ and waits for it to be ready.
// appName is the directory name in test/apps/, e.g., "httpserver".
// It returns a Server that can be stopped to get the output.
func (f *E2EFixture) StartApp(appName string, args ...string) *Server {
	fullPath := f.resolveAppPath(appName)
	Start(f.t, fullPath, args...)

	return &Server{
		t:       f.t,
		appPath: appName,
	}
}

// // RunClient runs a client application and waits for it to complete.
// // appPath is relative to the demo directory, e.g., "http/client".
// // Returns the application output.
// // Deprecated: Use RunApp for test/apps.
// func (f *E2EFixture) RunClient(appPath string, args ...string) string {
// 	return Run(f.t, f.resolvePath(appPath), args...)
// }

// RunApp runs a test application from test/apps/ and waits for it to complete.
// appName is the directory name in test/apps/, e.g., "httpclient".
// Returns the application output.
func (f *E2EFixture) RunApp(appName string, args ...string) string {
	return Run(f.t, f.resolveAppPath(appName), args...)
}

// RequireTraceCount asserts the expected number of traces were collected.
func (f *E2EFixture) RequireTraceCount(expected int) {
	stats := AnalyzeTraces(f.t, f.collector.Traces)
	require.Equal(f.t, expected, stats.TraceCount,
		"Expected %d traces, got %d. %s", expected, stats.TraceCount, stats.String())
}

// RequireSpansPerTrace asserts each trace has the expected number of spans.
func (f *E2EFixture) RequireSpansPerTrace(expected int) {
	stats := AnalyzeTraces(f.t, f.collector.Traces)
	for traceID, count := range stats.SpansPerTrace {
		require.Equal(f.t, expected, count,
			"Trace %s should have %d spans, got %d", traceID[:16], expected, count)
	}
}

// RequireSingleSpan asserts exactly 1 trace with 1 span and returns it.
// Use this for integration tests that expect a single span.
func (f *E2EFixture) RequireSingleSpan() ptrace.Span {
	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(1)
	spans := AllSpans(f.collector.Traces)
	require.Len(f.t, spans, 1, "Expected exactly 1 span")
	return spans[0]
}
