// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package gitcmd_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.opentelemetry.io/otelc/tools/bundlerepair/internal/gitcmd"
)

func TestArgs(t *testing.T) {
	runner := gitcmd.Runner{Dir: "/work", Config: []string{"merge.x.driver=true"}}
	got := runner.Args("merge", "--no-ff")
	want := []string{
		"-C", "/work",
		"-c", "core.hooksPath=/dev/null",
		"-c", "user.name=github-actions[bot]",
		"-c", "user.email=41898282+github-actions[bot]@users.noreply.github.com",
		"-c", "merge.x.driver=true",
		"merge", "--no-ff",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Args = %q, want %q", got, want)
	}
	if status := (gitcmd.Runner{}).Args("status"); slices.Contains(status, "-C") {
		t.Errorf("an empty Dir added -C: %q", status)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "git", "init", "--quiet", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

func TestOutputAndRun(t *testing.T) {
	dir := newRepo(t)
	runner := gitcmd.Runner{Dir: dir}
	out, err := runner.Output(t.Context(), "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(out) != "true" {
		t.Errorf("Output = %q, %v", out, err)
	}
	var stdout bytes.Buffer
	runner.Stdout = &stdout
	if err = runner.Run(
		t.Context(),
		"rev-parse",
		"--git-dir",
	); err != nil ||
		strings.TrimSpace(stdout.String()) != ".git" {
		t.Errorf("Run wrote %q, %v", stdout.String(), err)
	}
}

func TestErrorCapturesStderr(t *testing.T) {
	dir := newRepo(t)
	var tee bytes.Buffer
	runner := gitcmd.Runner{Dir: dir, Stderr: &tee}
	_, err := runner.Output(t.Context(), "rev-parse", "--verify", "no-such-ref")
	var gitErr *gitcmd.Error
	if !errors.As(err, &gitErr) {
		t.Fatalf("error = %v, want *gitcmd.Error", err)
	}
	if gitErr.Stderr == "" || !strings.Contains(err.Error(), gitErr.Stderr) {
		t.Errorf("the error text lacks stderr: %q", err)
	}
	if tee.Len() == 0 {
		t.Error("Stderr did not receive the output")
	}
	if _, ok := errors.AsType[*exec.ExitError](err); !ok {
		t.Error("the process error is not wrapped")
	}
}

func TestHooksAreDisabledAndIdentityIsFixed(t *testing.T) {
	dir := newRepo(t)
	marker := filepath.Join(t.TempDir(), "hook-ran")
	hook := "#!/bin/sh\ntouch " + marker + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "hooks", "pre-commit"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := gitcmd.Runner{Dir: dir}
	ctx := t.Context()
	if err := runner.Run(ctx, "add", "f"); err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(ctx, "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("a hook ran")
	}
	out, err := runner.Output(ctx, "log", "-1", "--format=%an <%ae>")
	want := "github-actions[bot] <41898282+github-actions[bot]@users.noreply.github.com>"
	if err != nil || strings.TrimSpace(out) != want {
		t.Errorf("author = %q, %v, want %q", out, err, want)
	}
}

func TestCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := (gitcmd.Runner{}).Run(ctx, "--version"); err == nil {
		t.Error("a canceled context did not stop the command")
	}
}
