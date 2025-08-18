// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

func RunCmdWithEnv(ctx context.Context, env []string, args ...string) error {
	path := args[0]
	args = args[1:]
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	err := cmd.Run()
	if err != nil {
		return ex.Errorf(err, "failed to run command %s", path)
	}
	return nil
}

func RunCmd(ctx context.Context, args ...string) error {
	return RunCmdWithEnv(ctx, nil, args...)
}

func IsWindows() bool {
	return runtime.GOOS == "windows"
}

func IsUnix() bool {
	return runtime.GOOS == "linux" || runtime.GOOS == "darwin"
}

func CopyFile(src, dst string) error {
	_, err := os.Stat(filepath.Dir(dst))
	if os.IsNotExist(err) {
		err = os.MkdirAll(filepath.Dir(dst), 0o755)
		if err != nil {
			return ex.Error(err)
		}
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return ex.Error(err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return ex.Error(err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return ex.Error(err)
	}
	return nil
}
