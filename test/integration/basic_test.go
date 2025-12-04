//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"path/filepath"
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
	}
	for _, e := range expect {
		require.Contains(t, output, e)
	}

	verifyGenericHookContextLogs(t, output)
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
