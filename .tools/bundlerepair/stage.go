// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/errs"
	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/gitcmd"
)

const (
	stageUsage = "Usage: bundlerepair stage <branch-dir> <trusted-dir> <base-sha>"
	stageArgs  = 3

	// baseMovedMessage explains why staging stopped. The prepare job captured
	// the base commit, so a newer trusted HEAD means main moved in between.
	baseMovedMessage = "main moved after the request was prepared; request the repair again"

	// writableDir lets the copy add entries to a directory whose final mode
	// is read-only. The copy sets the final mode after it fills the directory.
	writableDir = 0o700
)

// stagedTrees are the only trees that staging copies. The Makefile and the
// bundle tool stay those of the trusted checkout.
func stagedTrees() []string {
	return []string{"pkg", "instrumentation"}
}

// stage copies the bundled source trees from an untrusted checkout into the
// trusted one. It copies only plain files and directories. It checks the
// trusted base commit and every tree before it changes the trusted checkout.
func (a app) stage(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "--help" {
		_, err := fmt.Fprintln(a.stdout, stageUsage)
		return err
	}
	if len(args) != stageArgs || args[0] == "" || args[1] == "" || !sha1Pattern.MatchString(args[2]) {
		return errors.New("a branch directory, a trusted directory, and a full base commit SHA are required")
	}
	branchDir, trustedDir := filepath.Clean(args[0]), filepath.Clean(args[1])
	if !isDir(branchDir) || !isDir(trustedDir) {
		return errors.New("a branch directory and a trusted directory are required")
	}
	if err := checkTrustedBase(ctx, trustedDir, args[2]); err != nil {
		return err
	}
	return stageSources(branchDir, trustedDir)
}

// checkTrustedBase fails unless the trusted checkout is at baseSHA. The
// prepare job captured the base commit, so a newer trusted HEAD means main
// moved after the request. The build must then stop instead of publishing a
// bundle that the older Makefile and bundle tool produced.
func checkTrustedBase(ctx context.Context, trustedDir, baseSHA string) error {
	git := gitcmd.Runner{Dir: trustedDir}
	out, err := git.Output(ctx, "rev-parse", "HEAD")
	if err != nil {
		return errs.Wrap(err, "read the trusted HEAD")
	}
	if strings.TrimSpace(out) != baseSHA {
		return errors.New(baseMovedMessage)
	}
	return nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func stageSources(branchDir, trustedDir string) error {
	trees := stagedTrees()
	for _, tree := range trees {
		if err := checkTree(filepath.Join(branchDir, tree)); err != nil {
			return err
		}
	}
	for _, tree := range trees {
		if err := os.RemoveAll(filepath.Join(trustedDir, tree)); err != nil {
			return errs.Wrap(err, "remove the trusted "+tree+" tree")
		}
	}
	for _, tree := range trees {
		if err := copyTree(filepath.Join(branchDir, tree), filepath.Join(trustedDir, tree)); err != nil {
			return err
		}
	}
	return nil
}

// checkTree accepts a real directory that holds only plain files and
// directories. It never follows a link.
func checkTree(root string) error {
	info, err := os.Lstat(root)
	switch {
	case err == nil && info.Mode()&fs.ModeSymlink != 0:
		return errors.New(root + " is a symbolic link")
	case err == nil && !info.IsDir(), errors.Is(err, fs.ErrNotExist):
		return errors.New(root + " is not a directory")
	case err != nil:
		return errs.Wrap(err, "inspect "+root)
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() && !entry.IsDir() {
			return errors.New(path + " is not a regular file or a directory")
		}
		return nil
	})
}

// copyTree copies src to dst, which must not exist. It keeps the permission
// bits of every file and directory, and it does not apply the umask.
func copyTree(src, dst string) error {
	var dirs []dirPerm
	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			if err = os.Mkdir(target, writableDir); err != nil {
				return err
			}
			dirs = append(dirs, dirPerm{target, info.Mode().Perm()})
			return nil
		case entry.Type().IsRegular():
			return copyFile(path, target, info.Mode().Perm())
		default:
			// The check ran before the copy, so the tree changed in between.
			return errors.New(path + " is not a regular file or a directory")
		}
	})
	if err != nil {
		return errs.Wrap(err, "copy "+src)
	}
	// Children first, so a read-only parent does not block a child.
	for _, dir := range slices.Backward(dirs) {
		if err = os.Chmod(dir.path, dir.perm); err != nil {
			return errs.Wrap(err, "copy "+src)
		}
	}
	return nil
}

type dirPerm struct {
	path string
	perm fs.FileMode
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(
		dst,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		writableDir,
	)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if err = errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	return os.Chmod(dst, perm)
}
