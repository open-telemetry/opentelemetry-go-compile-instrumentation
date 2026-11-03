// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// bundlerepair holds the decision logic of the bundle repair workflows. The
// workflows build it from the trusted checkout and call one subcommand per
// step. It reads its input from environment variables and never runs code from
// a contributor checkout.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"
)

const (
	exitFailure = 1
	exitUsage   = 2

	helpFlag = "--help"
)

const usageText = `Usage: bundlerepair <command> [arguments]

Commands:
  authorize                          check who may request a bundle repair
  stage <branch-dir> <trusted-dir> <base-sha>   copy the bundled source trees into the trusted checkout
  merge <checkout> <base-sha>        merge the base commit without committing
  select [--budget]                  choose conflicting pull requests for automatic repair
  prepare                            capture and check the pull request of a repair
  publish [checkout]                 commit and push the regenerated bundle
  identity                           check the publisher token and report its account
  credentials                        choose between the App and the environment token
`

// app holds what the commands need from the process, so tests can replace it.
type app struct {
	getenv func(string) string
	// lookup tells an unset variable from an empty one.
	lookup func(string) (string, bool)
	now    func() time.Time
	stdout io.Writer
	stderr io.Writer
}

func main() {
	os.Exit(runMain())
}

func runMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	a := app{getenv: os.Getenv, lookup: os.LookupEnv, now: time.Now, stdout: os.Stdout, stderr: os.Stderr}
	return a.run(ctx, os.Args[1:])
}

// run executes one subcommand and returns the process exit code.
func (a app) run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(a.stderr, usageText)
		return exitUsage
	}
	command, rest := args[0], args[1:]
	var err error
	switch command {
	case "authorize":
		err = a.authorize(ctx, rest)
	case "stage":
		err = a.stage(ctx, rest)
	case "merge":
		err = a.merge(ctx, rest)
	case "select":
		err = a.selectRepairs(ctx, rest)
	case "prepare":
		err = a.prepare(ctx, rest)
	case "publish":
		err = a.publish(ctx, rest)
	case "identity":
		err = a.identity(ctx, rest)
	case "credentials":
		err = a.credentials(rest)
	case "-h", helpFlag, "help":
		_, _ = fmt.Fprint(a.stdout, usageText)
		return 0
	default:
		_, _ = fmt.Fprintf(a.stderr, "bundlerepair: unknown command %q\n\n", command)
		_, _ = fmt.Fprint(a.stderr, usageText)
		return exitUsage
	}
	if err != nil {
		_, _ = fmt.Fprintf(a.stderr, "bundlerepair %s: %v\n", command, err)
		return exitFailure
	}
	return 0
}
