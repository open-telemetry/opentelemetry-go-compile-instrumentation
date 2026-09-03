//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestLogsSlog(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "logslog", "go", "build", "-a")

	f := testutil.NewTestFixture(t, testutil.WithoutCollector())
	output := f.Run("logslog")

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

	logMessages := []string{
		"standard log message 1",
		"standard log message 2 with format",
		"standard log message 3",
	}
	for _, msg := range logMessages {
		require.Contains(t, output, msg, "Expected log message: %s", msg)
	}

	traceIDPattern := regexp.MustCompile(`trace_id=[a-f0-9]{32}`)
	matches := traceIDPattern.FindAllString(output, -1)
	require.NotEmpty(t, matches, "Expected trace_id to be injected into log messages")

	spanIDPattern := regexp.MustCompile(`span_id=[a-f0-9]{16}`)
	spanMatches := spanIDPattern.FindAllString(output, -1)
	require.NotEmpty(t, spanMatches, "Expected span_id to be injected into log messages")
}

func TestLogsLogrus(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "logslogrus", "go", "build", "-a")

	f := testutil.NewTestFixture(t, testutil.WithoutCollector())
	output := f.Run("logslogrus")

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

	traceIDPattern := regexp.MustCompile(`"trace_id":"[a-f0-9]{32}"`)
	matches := traceIDPattern.FindAllString(output, -1)
	require.NotEmpty(t, matches, "Expected trace_id to be injected into logrus messages")

	spanIDPattern := regexp.MustCompile(`"span_id":"[a-f0-9]{16}"`)
	spanMatches := spanIDPattern.FindAllString(output, -1)
	require.NotEmpty(t, spanMatches, "Expected span_id to be injected into logrus messages")
}

func TestLogsZerolog(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "logszerolog", "go", "build", "-a")

	f := testutil.NewTestFixture(t, testutil.WithoutCollector())
	output := f.Run("logszerolog")

	zerologMessages := []string{
		"trace message",
		"debug message",
		"info message",
		"warn message",
		"error message",
		"err with message",
		"log message",
		"logger print message",
		"logger printf message",
		"logger println message",
		"panic message",
		"fatal message",
	}

	type zerologEntry struct {
		Message string `json:"message"`
		TraceID string `json:"trace_id"`
		SpanID  string `json:"span_id"`
	}
	
	entries := make(map[string]zerologEntry)
	for line := range strings.Lines(output) {
		var entry zerologEntry
		err := json.Unmarshal([]byte(line), &entry)
		require.Nil(t, err, "Failed to unmarshal zerolog entry: %s", line)

		entries[strings.TrimSuffix(entry.Message, "\n")] = entry
	}

	loggers := []string{"loggerFromNew", "loggerFromInstantiated"}

	for _, logger := range loggers {
		for _, msg := range zerologMessages {
			entry, ok := entries[msg+" "+logger]
			require.True(t, ok, "Expected zerolog message: %s %s", msg, logger)
			require.NotZero(t, entry.TraceID, "Expected trace_id on zerolog message: %s %s", msg, logger)
			require.NotZero(t, entry.SpanID, "Expected span_id on zerolog message: %s %s", msg, logger)
		}
	}
}
