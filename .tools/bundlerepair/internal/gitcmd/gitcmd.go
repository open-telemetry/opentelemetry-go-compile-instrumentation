// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package gitcmd runs git in a checkout that holds contributor content.
// Every command runs with hooks disabled and with a fixed bot identity.
package gitcmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const (
	// BotName is the commit identity for merges that the workflows create.
	BotName = "github-actions[bot]"
	// BotEmail is the commit email for merges that the workflows create.
	BotEmail = "41898282+github-actions[bot]@users.noreply.github.com"
	// NoHooksPath points git at a path with no hooks.
	NoHooksPath = "/dev/null"
)

// Runner runs git commands in one directory.
type Runner struct {
	// Dir is the checkout. An empty Dir uses the current directory.
	Dir string
	// Config holds extra "key=value" settings that apply to every command.
	Config []string
	// Env holds extra "KEY=value" environment entries. They replace entries of
	// the same name in the process environment.
	Env []string
	// Stdout receives the standard output of Run. A nil value discards it.
	Stdout io.Writer
	// Stderr also receives the standard error output. A nil value keeps it only
	// in the returned error.
	Stderr io.Writer
}

// Error is a failed git command.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

// Error describes the failure with the captured standard error output.
func (e *Error) Error() string {
	message := fmt.Sprintf("git %s: %v", strings.Join(e.Args, " "), e.Err)
	if e.Stderr != "" {
		message += ": " + e.Stderr
	}
	return message
}

// Unwrap returns the process error, such as *exec.ExitError.
func (e *Error) Unwrap() error { return e.Err }

// Run runs git with args and writes its standard output to r.Stdout.
func (r Runner) Run(ctx context.Context, args ...string) error {
	return r.run(ctx, r.Stdout, args)
}

// Output runs git with args and returns its standard output.
func (r Runner) Output(ctx context.Context, args ...string) (string, error) {
	var stdout bytes.Buffer
	if err := r.run(ctx, &stdout, args); err != nil {
		return "", err
	}
	return stdout.String(), nil
}

// Args returns the full argument list for args, without the git program name.
func (r Runner) Args(args ...string) []string {
	full := make([]string, 0, len(args)+len(r.Config)*2+8) //nolint:mnd // fixed options below
	if r.Dir != "" {
		full = append(full, "-C", r.Dir)
	}
	full = append(full,
		"-c", "core.hooksPath="+NoHooksPath,
		"-c", "user.name="+BotName,
		"-c", "user.email="+BotEmail,
	)
	for _, setting := range r.Config {
		full = append(full, "-c", setting)
	}
	return append(full, args...)
}

func (r Runner) run(ctx context.Context, stdout io.Writer, args []string) error {
	full := r.Args(args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Stdout = stdout
	if len(r.Env) > 0 {
		cmd.Env = append(os.Environ(), r.Env...)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if r.Stderr != nil {
		cmd.Stderr = io.MultiWriter(&stderr, r.Stderr)
	}
	if err := cmd.Run(); err != nil {
		return &Error{Args: args, Stderr: strings.TrimSpace(stderr.String()), Err: err}
	}
	return nil
}
