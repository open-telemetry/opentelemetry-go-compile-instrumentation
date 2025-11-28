// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"context"
	"hash/crc32"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

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
		return ex.Wrapf(err, "failed to run command %q with args: %v", path, args)
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
			return ex.Wrap(err)
		}
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return ex.Wrap(err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return ex.Wrap(err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return ex.Wrap(err)
	}
	return nil
}

func CRC32(s string) string {
	crc32Hash := crc32.ChecksumIEEE([]byte(s))
	return strconv.FormatUint(uint64(crc32Hash), 10)
}

func ListFiles(dir string) ([]string, error) {
	var files []string
	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return ex.Wrap(err)
		}
		// Don't list files under hidden directories
		if strings.HasPrefix(info.Name(), ".") {
			return filepath.SkipDir
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	}
	err := filepath.Walk(dir, walkFn)
	if err != nil {
		return nil, ex.Wrap(err)
	}
	return files, nil
}

func WriteFile(filePath, content string) error {
	file, err := os.Create(filePath)
	if err != nil {
		return ex.Wrap(err)
	}
	defer func(file *os.File) {
		err = file.Close()
		if err != nil {
			ex.Fatal(err)
		}
	}(file)

	_, err = file.WriteString(content)
	if err != nil {
		return ex.Wrap(err)
	}
	return nil
}

func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
