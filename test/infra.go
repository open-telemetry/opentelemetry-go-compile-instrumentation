// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/stretchr/testify/require"
)

func RunCmd(dir string, args ...string) (string, error) {
	path := args[0]
	args = args[1:]
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}

func RunInstrument(t *testing.T, appDir string, args ...string) {
	binName := "otel"
	if util.IsWindows() {
		binName += ".exe"
	}
	otelPath := filepath.Join("..", "..", binName)
	args = append([]string{otelPath}, args...)
	out, err := RunCmd(appDir, args...)
	require.NoError(t, err, out)
}

func RunApp(t *testing.T, dir string) string {
	out, err := RunCmd(dir, "./"+filepath.Base(dir))
	require.NoError(t, err, out)
	return out
}
