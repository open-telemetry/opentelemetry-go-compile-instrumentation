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
	"time"

	"go.opentelemetry.io/otelc/tool/ex"
)

// cancelGrace is how long a canceled command gets to exit after SIGINT
// before cmd.Cancel's caller falls back to killing it. os/exec's default
// Cancel (used when WaitDelay is set but Cancel is not overridden) is
// cmd.Process.Kill, so this still ends in a hard kill if the process does
// not exit in time.
const cancelGrace = 10 * time.Second

func runCmd(ctx context.Context, dir string, env []string, args ...string) error {
	if len(args) == 0 {
		return ex.New("no command specified")
	}
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

	// A canceled context otherwise hard-kills only this direct child: Ctrl-C
	// reaches the whole foreground process group, but exec.CommandContext's
	// default Cancel sends SIGKILL to this process alone, so any toolexec or
	// compile/link child it spawned can keep running after this process
	// exits. Signaling it to interrupt first gives it a chance to shut down
	// its own children the way it already does for Ctrl-C, with WaitDelay
	// falling back to a hard kill if it ignores that. Interrupt is not
	// implemented on Windows (os/exec.Signal would just return an error),
	// so Windows keeps the default Kill-only behavior.
	if !IsWindows() {
		cmd.Cancel = func() error {
			return cmd.Process.Signal(os.Interrupt)
		}
		cmd.WaitDelay = cancelGrace
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

func isUnix() bool {
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

// WriteFileAtomic writes data to a file atomically by first writing to a temporary file and then renaming it.
// Permission precedence: explicit perm argument > existing file's permissions > default 0644.
// A symbolic link is followed, so the file it points to is replaced and the
// link stays in place. Renaming over the link itself would turn it into a
// regular file and leave its target as it was.
func WriteFileAtomic(filePath string, data []byte, perm ...os.FileMode) error {
	const defaultPerm = 0o644
	if resolved, err := filepath.EvalSymlinks(filePath); err == nil {
		filePath = resolved
	}
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
