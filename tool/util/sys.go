// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

func RunCmd(args ...string) error {
	path := args[0]
	args = args[1:]
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return ex.Errorf(err, "failed to run command %s", path)
	}
	return nil
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
