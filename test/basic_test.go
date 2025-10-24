// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBasic(t *testing.T) {
	appDir := filepath.Join("..", "demo", "basic")

	RunInstrument(t, appDir, "go", "build", "-a")
	output := RunApp(t, appDir)
	expect := []string{
		"Every is called",
		"MyStruct.Example",
		"traceID: 123, spanID: 456",
		"[MyHook]",
		"=setupOpenTelemetry=",
		"RawCode",
	}
	for _, e := range expect {
		require.Contains(t, output, e)
	}
}
