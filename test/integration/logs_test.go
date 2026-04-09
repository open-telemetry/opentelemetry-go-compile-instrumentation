//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

// TestLogsSlog tests slog and log instrumentation
func TestLogsSlog(t *testing.T) {
	appDir := filepath.Join("..", "..", "test", "apps", "logslog")

	testutil.Build(t, appDir, "go", "build", "-a")
	output := testutil.Run(t, appDir)

	// Verify slog messages are present
	slogMessages := []string{
		"slog info message with context",
		"slog warn message with context",
		"slog error message with context",
		"slog info message without context",
		"slog warn message without context",
	}
	for _, msg := range slogMessages {
		require.Contains(t, output, msg, "Expected slog message: %s", msg)
	}

	// Verify standard log messages are present
	logMessages := []string{
		"standard log message 1",
		"standard log message 2 with format",
		"standard log message 3",
	}
	for _, msg := range logMessages {
		require.Contains(t, output, msg, "Expected log message: %s", msg)
	}

	// Verify trace context is injected into slog messages (via trace_id key)
	traceIDPattern := regexp.MustCompile(`trace_id=[a-f0-9]{32}`)
	matches := traceIDPattern.FindAllString(output, -1)
	require.NotEmpty(t, matches, "Expected trace_id to be injected into log messages")

	// Verify span context is present
	spanIDPattern := regexp.MustCompile(`span_id=[a-f0-9]{16}`)
	spanMatches := spanIDPattern.FindAllString(output, -1)
	require.NotEmpty(t, spanMatches, "Expected span_id to be injected into log messages")
}

// TestLogsLogrus tests logrus instrumentation
func TestLogsLogrus(t *testing.T) {
	appDir := filepath.Join("..", "..", "test", "apps", "logslogrus")

	testutil.Build(t, appDir, "go", "build", "-a")
	output := testutil.Run(t, appDir)

	// Verify logrus messages are present
	logrusMessages := []string{
		"logrus info message from New logger",
		"logrus info with field",
		"logrus standard info message",
		"logrus standard info with field",
		"logrus entry info message",
		"logrus info message with context",
	}
	for _, msg := range logrusMessages {
		require.Contains(t, output, msg, "Expected logrus message: %s", msg)
	}

	// Verify trace context is injected into logrus messages
	// logrus uses JSON format, so we look for trace_id field
	traceIDPattern := regexp.MustCompile(`"trace_id":"[a-f0-9]{32}"`)
	matches := traceIDPattern.FindAllString(output, -1)
	require.NotEmpty(t, matches, "Expected trace_id to be injected into logrus messages")

	// Verify span context is present
	spanIDPattern := regexp.MustCompile(`"span_id":"[a-f0-9]{16}"`)
	spanMatches := spanIDPattern.FindAllString(output, -1)
	require.NotEmpty(t, spanMatches, "Expected span_id to be injected into logrus messages")
}
