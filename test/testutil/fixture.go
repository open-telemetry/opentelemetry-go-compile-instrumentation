// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// TestFixture provides common setup for e2e and integration tests.
type TestFixture struct {
	t         *testing.T
	collector *Collector

	serviceName   string
	skipCollector bool

	// env holds per-fixture environment overrides (KEY=VALUE form). Applied
	// when spawning processes via f.Start/f.Run so parallel tests do not
	// race on os.Environ via t.Setenv.
	env []string
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

// NewTestFixture creates a new test fixture. It starts a collector (unless
// disabled) and configures OTEL env vars that will be applied to processes
// spawned via this fixture. The env is fixture-local — it does not touch
// os.Environ, which keeps the fixture safe for use under t.Parallel().
func NewTestFixture(t *testing.T, opts ...TestFixtureOption) *TestFixture {
	f := &TestFixture{
		t:           t,
		serviceName: "test-service",
	}

	for _, opt := range opts {
		opt(f)
	}

	if !f.skipCollector {
		f.collector = StartCollector(t)

		// Clear signal-specific endpoints so OTEL_EXPORTER_OTLP_ENDPOINT wins
		f.SetEnv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
		f.SetEnv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
		f.SetEnv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")
		f.SetEnv("OTEL_SERVICE_NAME", f.serviceName)
		f.SetEnv("OTEL_TRACES_EXPORTER", "otlp")
		f.SetEnv("OTEL_EXPORTER_OTLP_ENDPOINT", f.collector.URL)
		f.SetEnv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
		f.SetEnv("OTEL_GO_SIMPLE_SPAN_PROCESSOR", "true")
	}

	return f
}

// SetEnv adds (or replaces) an environment override for processes spawned by
// this fixture. Unlike t.Setenv it does not modify os.Environ, so it is safe
// under t.Parallel().
func (f *TestFixture) SetEnv(key, value string) {
	prefix := key + "="
	entry := prefix + value
	for i, e := range f.env {
		if strings.HasPrefix(e, prefix) {
			f.env[i] = entry
			return
		}
	}
	f.env = append(f.env, entry)
}

// Env returns the full environment to pass to a spawned subprocess:
// os.Environ() merged with the fixture's overrides (overrides win on dup keys
// because exec.Cmd respects the last occurrence).
func (f *TestFixture) Env() []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+len(f.env))
	out = append(out, base...)
	out = append(out, f.env...)
	return out
}

// Traces returns the collected traces for assertions.
func (f *TestFixture) Traces() ptrace.Traces {
	return f.collector.GetTraces()
}

// CollectorURL returns the collector endpoint URL.
func (f *TestFixture) CollectorURL() string {
	return f.collector.URL
}

// Server represents a running server process.
type Server struct {
	t       *testing.T
	appPath string

	*exec.Cmd
}

// Start starts a test application from test/apps/. The binary is
// expected to be built using Build.
func (f *TestFixture) Start(appName string, args ...string) *Server {
	cmd := Start(f.t, "", appName, f.Env(), args...)
	return &Server{t: f.t, appPath: appName, Cmd: cmd}
}

// Run runs a application and returns its output. The binary is
// expected to be built using Build.
func (f *TestFixture) Run(appName string, args ...string) string {
	return Run(f.t, "", appName, f.Env(), args...)
}

// BuildAndStart builds the named app and then starts it. Kept for e2e test
// backward compatibility. Integration tests build applications once and reuse them.
func (f *TestFixture) BuildAndStart(appName string, args ...string) *Server {
	Build(f.t, "", appName, "go", "build", "-a")
	return f.Start(appName, args...)
}

// BuildAndRun builds the named app and runs it, returning its output. Kept for
// e2e test backward compatibility. Integration tests build applications once and reuse them.
func (f *TestFixture) BuildAndRun(appName string, args ...string) string {
	Build(f.t, "", appName, "go", "build", "-a")
	return f.Run(appName, args...)
}

// RequireTraceCount asserts the expected number of traces were collected.
func (f *TestFixture) RequireTraceCount(expected int) {
	stats := AnalyzeTraces(f.t, f.collector.GetTraces())
	require.Equal(f.t, expected, stats.TraceCount,
		"Expected %d traces, got %d. %s", expected, stats.TraceCount, stats.String())
}

// RequireSpansPerTrace asserts each trace has the expected number of spans.
func (f *TestFixture) RequireSpansPerTrace(expected int) {
	stats := AnalyzeTraces(f.t, f.collector.GetTraces())
	for traceID, count := range stats.SpansPerTrace {
		require.Equal(f.t, expected, count,
			"Trace %s should have %d spans, got %d", traceID[:16], expected, count)
	}
}

// RequireSingleSpan asserts exactly 1 trace with 1 span and returns it.
func (f *TestFixture) RequireSingleSpan() ptrace.Span {
	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(1)
	spans := AllSpans(f.collector.GetTraces())
	require.Len(f.t, spans, 1, "Expected exactly 1 span")
	return spans[0]
}
