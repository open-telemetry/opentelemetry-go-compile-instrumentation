// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"bytes"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func RunSelfTest(t *testing.T, testName, env string) (int, string) {
	t.Helper()

	exe, err := os.Executable()
	require.NoError(t, err)

	cmd := exec.Command(exe, "-test.run="+testName)
	cmd.Env = append(os.Environ(), env+"=1")

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	_ = cmd.Run()
	return cmd.ProcessState.ExitCode(), out.String()
}
