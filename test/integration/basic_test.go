//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

func TestBasic(t *testing.T) {
	appDir := filepath.Join("..", "..", "demo", "basic")

	app.Build(t, appDir, "go", "build", "-a")
	output := app.Run(t, appDir)
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
		"=setupOpenTelemetry=",
		"RawCode",
		"funcName:Example",
		"packageName:main",
		"paramCount:1",
		"returnValCount:0",
		"isSkipCall:false",
		"Ellipsis",
		"Hello from stdio",
		"Underscore",
	}
	for _, e := range expect {
		require.Contains(t, output, e)
	}

	verifyGenericHookContextLogs(t, output)
	verifyTracePropagationBetweenFunctionAAndB(t, output)
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
