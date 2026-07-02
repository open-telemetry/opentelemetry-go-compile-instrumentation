//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestBasic(t *testing.T) {
	t.Parallel()

	appsDir := filepath.Join("..", "..", "demo", "app")
	testutil.Build(t, appsDir, "basic", "go", "build", "-a")
	output := testutil.Run(t, appsDir, "basic", nil)
	expect := []string{
		"Every1",
		"Every3",
		"MyStruct.Example",
		"MyStruct.Example2",
		"GenericExample before hook",
		"Hello, Generic World! 1 2",
		"GenericExample after hook",
		"traceID: 123, spanID: 456",
		"GenericRecvExample before hook",
		"Hello, Generic Recv World!",
		"GenericRecvExample after hook",
		"traceID: 123, spanID: 456",
		"[MyHook]",
		"RawCode",
		"funcName:Example",
		"packageName:main",
		"paramCount:1",
		"returnValCount:0",
		"isSkipCall:false",
		"Ellipsis",
		"Hello from stdio",
		"Underscore",
		"AutoDetect: 00000000-0000-0000-0000-000000000000",
		"UnnamedBefore 42 2.7",
	}
	for _, e := range expect {
		require.Contains(t, output, e)
	}

	verifyGenericHookContextLogs(t, output)
	verifyExportedHelloWorldSpan(t, output)
	verifyTracePropagationBetweenFunctionAAndB(t, output)
}

type exportedSpan struct {
	Name                 string               `json:"Name"`
	SpanContext          spanContext          `json:"SpanContext"`
	Attributes           []spanAttribute      `json:"Attributes"`
	InstrumentationScope instrumentationScope `json:"InstrumentationScope"`
}

type spanContext struct {
	TraceID string `json:"TraceID"`
	SpanID  string `json:"SpanID"`
}

type spanAttribute struct {
	Key   string             `json:"Key"`
	Value spanAttributeValue `json:"Value"`
}

type spanAttributeValue struct {
	Value any `json:"Value"`
}

type instrumentationScope struct {
	Name string `json:"Name"`
}

func verifyGenericHookContextLogs(t *testing.T, output string) {
	expectedGenericLogs := []string{
		"[Generic] Function: main.GenericExample",
		"[Generic] Param count: 2",
		"[Generic] Skip call: false",
		"[Generic] Data from Before: test-data",
		"[Generic] Return value count: 1",
		"[Generic] SetParam panic (expected): SetParam is unsupported for generic functions",
		"[Generic] SetReturnVal panic (expected): SetReturnVal is unsupported for generic functions",
	}
	for _, log := range expectedGenericLogs {
		require.Contains(t, output, log, "Expected generic HookContext log: %s", log)
	}
}

func verifyExportedHelloWorldSpan(t *testing.T, output string) {
	t.Helper()

	span := findExportedSpan(t, output, "hello-world")

	require.NotEmpty(t, span.SpanContext.TraceID, "expected hello-world span to have a trace ID")
	require.NotEqual(t,
		strings.Repeat("0", 32),
		span.SpanContext.TraceID,
		"expected hello-world span trace ID to be non-zero",
	)
	require.NotEmpty(t, span.SpanContext.SpanID, "expected hello-world span to have a span ID")
	require.NotEqual(t,
		strings.Repeat("0", 16),
		span.SpanContext.SpanID,
		"expected hello-world span ID to be non-zero",
	)
	require.Equal(t,
		"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/instrumentation/basic",
		span.InstrumentationScope.Name,
	)

	attrs := make(map[string]any, len(span.Attributes))
	for _, attr := range span.Attributes {
		attrs[attr.Key] = attr.Value.Value
	}
	require.Contains(t, attrs, "hello.path")
	require.Equal(t, "/api/hello", attrs["hello.path"])

	require.Contains(t, attrs, "hello.param.name")
	require.Equal(t, "world", attrs["hello.param.name"])

	require.Contains(t, attrs, "hello.status")
	require.EqualValues(t, 200, attrs["hello.status"])
}

func findExportedSpan(t *testing.T, output, name string) exportedSpan {
	t.Helper()

	for _, line := range strings.Split(output, "\n") {
		var span exportedSpan
		if err := json.Unmarshal([]byte(line), &span); err != nil {
			continue
		}
		if span.Name == name {
			return span
		}
	}

	require.Failf(t, "span not found", "expected exported span %q in basic demo output", name)
	return exportedSpan{}
}

func verifyTracePropagationBetweenFunctionAAndB(t *testing.T, output string) {
	traceA, spanA := extractSpanInfo(t, output, "FunctionABefore")
	traceB, spanB := extractSpanInfo(t, output, "FunctionBBefore")

	require.Equal(t, traceA, traceB, "expected FunctionA and FunctionB to share the same trace ID")
	require.NotEqual(t, spanA, spanB, "expected FunctionA and FunctionB to have different span IDs")
}

//nolint:revive // just a helper function to extract span info from the output
func extractSpanInfo(t *testing.T, output, funcName string) (string, string) {
	re := regexp.MustCompile(funcName + `: TraceID: ([0-9a-f]{32}), SpanID: ([0-9a-f]{16})`)
	match := re.FindStringSubmatch(output)
	require.Len(t, match, 3, "expected log line for %s with trace and span IDs", funcName)
	return match[1], match[2]
}
