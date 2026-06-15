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

func runCmd(ctx context.Context, dir string, env []string, args ...string) error {
	path := args[0]
	args = args[1:]
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if dir != "" {
		cmd.Dir = dir
	}
	if env != nil {
		cmd.Env = env
	}

	err := cmd.Run()
	if err != nil {
		return ex.Wrapf(err, "failed to run command %q in dir '%q' with args: %v", path, dir, args)
	}
	return nil
}

// RunCmdWithEnv executes a command with custom environment variables.
func RunCmdWithEnv(ctx context.Context, env []string, args ...string) error {
	return runCmd(ctx, "", env, args...)
}

// RunCmd executes a command with the default environment.
func RunCmd(ctx context.Context, args ...string) error {
	return runCmd(ctx, "", nil, args...)
}

// RunCmdInDir executes a command in a specific directory.
func RunCmdInDir(ctx context.Context, dir string, args ...string) error {
	return runCmd(ctx, dir, nil, args...)
}

func IsWindows() bool {
	return runtime.GOOS == "windows"
}

func IsUnix() bool {
	return runtime.GOOS == "linux" || runtime.GOOS == "darwin"
}

func CopyFile(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return ex.Wrapf(err, "failed to stat source file %q", src)
	}

	dstInfo, err := os.Stat(dst)
	if err == nil {
		// Avoid self-copy which would otherwise truncate the file.
		if os.SameFile(srcInfo, dstInfo) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return ex.Wrapf(err, "failed to stat destination file %q", dst)
	}

	err = os.MkdirAll(filepath.Dir(dst), 0o755)
	if err != nil {
		return ex.Wrapf(err, "failed to create directory for file %q", dst)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return ex.Wrapf(err, "failed to open source file %q", src)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return ex.Wrapf(err, "failed to create destination file %q", dst)
	}

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		_ = dstFile.Close()
		return ex.Wrapf(err, "failed to copy file from %q to %q", src, dst)
	}

	err = dstFile.Close()
	if err != nil {
		return ex.Wrapf(err, "failed to close destination file %q", dst)
	}

	err = os.Chmod(dst, srcInfo.Mode().Perm())
	if err != nil {
		return ex.Wrapf(err, "failed to change permissions for file %q", dst)
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
		if path != dir && strings.HasPrefix(info.Name(), ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}

			return nil
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

// WriteFileAtomic writes data to a file atomically by first writing to a temporary file and then renaming it.
// If the target file already exists, it preserves its permissions. Otherwise, it uses the provided permissions or defaults to 0644.
func WriteFileAtomic(filePath string, data []byte, perm ...os.FileMode) error {
	const defaultPerm = 0o644
	mode := os.FileMode(defaultPerm)
	if fi, statErr := os.Stat(filePath); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if len(perm) > 0 {
		mode = perm[0]
	}

	tmp, createErr := os.CreateTemp(filepath.Dir(filePath), filepath.Base(filePath)+".tmp-*")
	if createErr != nil {
		return ex.Wrapf(createErr, "failed to create temporary file for %s", filePath)
	}

	tmpPath := tmp.Name()
	defer func() {
		// If Rename succeeds this will fail with ENOENT, which is fine.
		_ = os.Remove(tmpPath)
	}()

	if _, writeErr := tmp.Write(data); writeErr != nil {
		_ = tmp.Close()
		return ex.Wrapf(writeErr, "failed to write temporary file %s", tmpPath)
	}

	if closeErr := tmp.Close(); closeErr != nil {
		return ex.Wrapf(closeErr, "failed to close temporary file %s", tmpPath)
	}

	if chmodErr := os.Chmod(tmpPath, mode); chmodErr != nil {
		return ex.Wrapf(chmodErr, "failed to set permissions on temporary file %s", tmpPath)
	}

	// Atomic replacement on Unix-like systems when source and destination are on
	// the same filesystem. On non-Unix platforms, os.Rename may replace the file
	// but the operation is not guaranteed to be atomic.
	if renameErr := os.Rename(tmpPath, filePath); renameErr != nil {
		return ex.Wrapf(renameErr, "failed to atomically replace %s", filePath)
	}

	return nil
}

func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func NormalizePath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}
